package main

import (
	"github.com/go-ocf/go-coap"
)

func defaultHandler(s coap.ResponseWriter, req *coap.Request) {
	// handle message from tcp-client
	sendResponse(s, req.Client, coap.NotFound, nil)
}
