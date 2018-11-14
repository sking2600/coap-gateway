package main

import (
	coap "github.com/go-ocf/go-coap"
)

func oicSecSessionHandler(s coap.ResponseWriter, req *coap.Request) {
	sendResponse(s, req.Client, coap.Changed, nil)
}
