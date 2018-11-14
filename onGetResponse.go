package main

import coap "github.com/go-ocf/go-coap"

func onGetResponse(req *coap.Request) {
	decodeMsgToDebug(req.Msg, "onGetResponse")
}
