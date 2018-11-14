package main

import (
	coap "github.com/go-ocf/go-coap"
	"github.com/ugorji/go/codec"
)

func oicSecAccountHandler(s coap.ResponseWriter, req *coap.Request) {
	m := map[string]interface{}{
		"uid":         "00000000-0000-0000-0000-000000000000",
		"accesstoken": "abc",
	}

	var out []byte
	err := codec.NewEncoderBytes(&out, new(codec.CborHandle)).Encode(m)
	if err != nil {
		log.Errorf("Cannot marshal payload for client %v: %v", req.Client.RemoteAddr(), err)
		sendResponse(s, req.Client, coap.InternalServerError, nil)
		return
	}

	sendResponse(s, req.Client, coap.Changed, out)
}
