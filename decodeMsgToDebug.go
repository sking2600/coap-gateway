package main

import (
	"bytes"

	coap "github.com/go-ocf/go-coap"
	"github.com/ugorji/go/codec"
)

func decodeMsgToDebug(resp coap.Message, tag string) {
	var m interface{}
	log.Debug(
		"\n-------------------", tag, "------------------\n",
		"Path: ", resp.PathString(), "\n",
		"Code: ", resp.Code(), "\n",
		"Type: ", resp.Type(), "\n",
		"Query: ", resp.Options(coap.URIQuery), "\n",
		"ContentFormat: ", resp.Options(coap.ContentFormat),
	)
	err := codec.NewDecoderBytes(resp.Payload(), new(codec.CborHandle)).Decode(&m)
	if err != nil {
		log.Debugf("RAW:\n%v\n", resp.Payload())
	} else {
		bw := new(bytes.Buffer)
		h := new(codec.JsonHandle)
		h.BasicHandle.Canonical = true
		enc := codec.NewEncoder(bw, h)
		err = enc.Encode(m)
		if err != nil {
			log.Errorf("Cannot encode %v to JSON: %v", m, err)
		}
		log.Debugf("JSON:\n%v\n", bw.String())
	}
}
