package main

import coap "github.com/go-ocf/go-coap"

func onObserveNotification(req *coap.Request) {
	decodeMsgToDebug(req.Msg, "onObserveNotification")
}
