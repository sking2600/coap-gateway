package main

import (
	"bytes"
	"log"
	"time"

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

func sendReply(s coap.Session, req coap.Message) {
	resType := coap.NonConfirmable
	if req.IsConfirmable() {
		resType = coap.Acknowledgement
	}
	response := s.NewMessage(coap.MessageParams{
		Type:      resType,
		Code:      coap.NotFound,
		MessageID: req.MessageID(),
		Token:     req.Token(),
	})
	err := s.WriteMsg(response, time.Hour)
	if err != nil {
		log.Printf("Cannot send reply to %v: %v", s.RemoteAddr(), err)
	}
}

//DefaultHandler default handler for requests
func DefaultHandler(s coap.Session, req coap.Message) {
	// handle message from tcp-client
	decodeMsg(req, "REQUEST-CLIENT")
	sendReply(s, req)
}
