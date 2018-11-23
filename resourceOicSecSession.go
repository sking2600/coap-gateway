package main

import (
	coap "github.com/go-ocf/go-coap"
)

var oicSecSession = "/oic/sec/session"

func oicSecSessionHandler(s coap.ResponseWriter, req *coap.Request, server *Server) {
	sendResponse(s, req.Client, coap.Changed, nil)
}
