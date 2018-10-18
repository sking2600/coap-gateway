package main

import (
	"bytes"
	"log"

	"github.com/go-ocf/go-coap"
	"github.com/ugorji/go/codec"
)

func decodeMsg(resp coap.Message, tag string) {
	var m interface{}
	log.Printf("-------------------%v-----------------\n", tag)
	log.Printf("Path: %v\n", resp.PathString())
	err := codec.NewDecoderBytes(resp.Payload(), new(codec.CborHandle)).Decode(&m)
	if err != nil {
		log.Printf("RAW:\n%v\n", resp.Payload())
	} else {
		bw := new(bytes.Buffer)
		h := new(codec.JsonHandle)
		h.BasicHandle.Canonical = true
		enc := codec.NewEncoder(bw, h)
		err = enc.Encode(m)
		if err != nil {
			panic(err)
		}
		log.Printf("JSON:\n%v\n", bw.String())
	}
}

func sendReply(s coap.ResponseWriter, req *coap.Request) {
	s.SetCode(coap.NotFound)
	_, err := s.Write(nil)
	if err != nil {
		log.Printf("Cannot send reply to %v: %v", req.Client.RemoteAddr(), err)
	}
}

//DefaultHandler default handler for requests
func DefaultHandler(s coap.ResponseWriter, req *coap.Request) {
	// handle message from tcp-client
	decodeMsg(req.Msg, "REQUEST-CLIENT")
	sendReply(s, req)
}
