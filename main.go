package main

import (
  "io"
  "log"
  "fmt"
  "time"
  "runtime"
  "strconv"

  "net/http"
  "math/rand"
  "encoding/json"

  "github.com/gobwas/ws"
  "github.com/gobwas/ws/wsutil"

  "github.com/go-redis/redis"
)

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandStringRunes(n int) string {
  b := make([]rune, n)
  for i := range b {
    b[i] = letterRunes[rand.Intn(len(letterRunes))]
  }
  return string(b)
}

type ADNMetaRespone struct {
  Type string `json:"type"`
  Connection_id string `json:"connection_id"`
}
type ADNPingDataRespone struct {
  Id uint64 `json:"id"`
}
type ADNRepsonse struct {
  Meta ADNMetaRespone `json:"meta"`
  //Data string `json:"data"`
}
type ADNPingRepsonse struct {
  Meta ADNMetaRespone `json:"meta"`
  Data ADNPingDataRespone `json:"data"`
}

func main() {
  rand.Seed(time.Now().UnixNano())
  redisClient := redis.NewClient(&redis.Options{
    Addr:     "192.168.253.145:6379",
    Password: "", // no password set
    DB:       0,  // use default DB
  })
  pong, err := redisClient.Ping().Result()
  fmt.Println(pong, err)

  log.Println("AppDotNetWS v 0.2 TLS server starting on port 8000")
  http.ListenAndServeTLS(":8000", "/root/.acme.sh/api.sapphire.moe/fullchain.cer", "/root/.acme.sh/api.sapphire.moe/api.sapphire.moe.key", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    log.Println("Request", r.RemoteAddr)

    reqConnId := r.URL.Query().Get("connection_id")
    if (reqConnId != "") {
      // FIXME: if reqConnId is set, we need to boot any others using it
      // check redis, msg redis to disconnect
      // then reuse
      // otherwise just recreate
      log.Println("reqConnId", reqConnId)
    }

    var connectionId = RandStringRunes(64)
    // now connectionId is sorted

    token      := r.URL.Query().Get("access_token")
    // we need to tie token to connectionId
    var connTokenKey string
    connTokenKey += "token_"
    connTokenKey += connectionId
    rErr := redisClient.Set(connTokenKey, token, 0).Err()
    if rErr != nil {
        panic(rErr)
    }
    // we need to tie auto_delete to connectionId
    autoDelete := r.URL.Query().Get("auto_delete")
    var autoDelKey string
    autoDelKey += "autoDelete_"
    autoDelKey += connectionId
    rErr = redisClient.Set(autoDelKey, autoDelete, 0).Err()
    if rErr != nil {
        panic(rErr)
    }

    // javascript can't read websocket headers
    header := http.Header{
      "X-Go-Version": []string{runtime.Version()},
      "Connection-Id": []string{connectionId},
    }

    conn, _, _, err := ws.UpgradeHTTP(r, w, header)
    if err != nil {
      // handle error
    }

    pubsub := redisClient.Subscribe(connectionId)
    // Wait for subscription to be created before publishing message.
    subscr, err := pubsub.ReceiveTimeout(time.Second)
    if err != nil {
        panic(err)
    }
    fmt.Println(subscr)

    // one thread to pump redis
    go func() {
      defer pubsub.Close()
      for {
        msg, err := pubsub.ReceiveMessage()
        if err != nil {
            panic(err)
        }

        fmt.Println(msg.Channel, msg.Payload)
        //var dat map[string]interface{}
        var dat ADNRepsonse
        if err := json.Unmarshal([]byte(msg.Payload), &dat); err != nil {
          fmt.Println("Couldnt Unmarshal Payload into ADNRepsonse")
        }
        fmt.Println("MetaType", dat)
        if (dat.Meta.Type == "ping") {
          var res ADNPingRepsonse
          if err := json.Unmarshal([]byte(msg.Payload), &res); err != nil {
            fmt.Println("Couldnt Unmarshal Payload into ADNPingRepsonse", err)
            continue // we just wont pong
          }
          fmt.Println("pong!")
          redisClient.Publish("AppDotNetWS", "pong_"+connectionId+"_"+strconv.FormatUint(res.Data.Id, 10))
          continue
        }
        // demarshall
        err = wsutil.WriteServerMessage(conn, ws.OpText, []byte(msg.Payload))
        if err != nil {
          // handle error
          log.Println("redis write message err", err)

          redisClient.Del(connTokenKey)
          redisClient.Publish("AppDotNetWS", "disconnect_"+connectionId)
          pubsub.Close()
          // we need to kill the websocket too tbh
          conn.Close()
          break
        }
      }
    }()

    // another thread to listen to socket and handle closing the socket
    go func() {
      defer conn.Close()
      log.Println("WebSocket Connected", conn.RemoteAddr(), connectionId)

      var (
        state  = ws.StateServerSide
        reader = wsutil.NewReader(conn, state)
        writer = wsutil.NewWriter(conn, state, ws.OpText)
      )
      // javascript can't read websocket headers
      // The user stream endpoint will return the negotiated connection_id in HTTP headers (https)
      // or initial message (websocket).
      // send initial JSON
      msg := []byte(`{
        "meta": {
          "connection_id": "` + connectionId + `"
        },
        "data": {
        }
      }`)
      err = wsutil.WriteServerMessage(conn, ws.OpText, msg)
      if err != nil {
        // handle error
        log.Println("conn write message err", err)
      }
      for {
        header, err := reader.NextFrame()
        if err != nil {
          // handle error
          log.Println("frame err", err)
        }
        if header.OpCode == ws.OpClose {
          log.Println("opclose closing connection", conn.RemoteAddr())
          redisClient.Del(connTokenKey)
          redisClient.Publish("AppDotNetWS", "disconnect_"+connectionId)
          conn.Close()
          break
        }
        // https://godoc.org/github.com/gobwas/ws#Header
        log.Println("Frame Header", header)
        log.Println("Frame Content", reader)
        var buff [32 * 1024]byte
        if (header.OpCode.IsControl()) {
          log.Println("control frame", reader)
        } else {
          if (header.Length > 32 * 1024) {
          log.Println("skipping large frame")
            reader.Discard()
          } else {
            _, err = io.ReadFull(reader, buff[0:header.Length])
            if err == nil {
              s := string(buff[:header.Length])
              if header.OpCode.IsData() {
                log.Println("data frame", s)
              }
            }
          }
        }
        if err != nil {
          log.Println("err is closing connection", conn.RemoteAddr())
          redisClient.Del(connTokenKey)
          redisClient.Publish("AppDotNetWS", "disconnect_"+connectionId)
          //redisClient.Publish(connectionId, "disconnect")
          conn.Close()
          break
        }

        // Reset writer to write frame with right operation code.
        writer.Reset(conn, state, header.OpCode)
      }
    }()
  }))
}
