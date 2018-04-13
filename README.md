AppDotNetWS

Written in golang for efficency. Good for up to 32k or so websockets.
Communicates with AppDotNetAPI (node.js) via redis (pub/sub)

We generate the connectionId and communicates back and forth over redis to set various settings.
Receives events via redis and relays events to AppDotNetAPI backend.

TODO:
- add config: https://github.com/jinzhu/configor
- add logging:
https://github.com/uber-go/zap
https://github.com/op/go-logging
https://github.com/sirupsen/logrus
- remove elses?
