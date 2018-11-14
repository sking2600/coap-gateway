package main

import (
	"fmt"
	"strconv"
	"strings"

	coap "github.com/go-ocf/go-coap"
	"github.com/ugorji/go/codec"
)

const observable = 2

func parsePostPayload(msg coap.Message) (wkRd map[string]interface{}, err error) {
	err = codec.NewDecoderBytes(msg.Payload(), new(codec.CborHandle)).Decode(&wkRd)
	if err != nil {
		err = fmt.Errorf("Cannot decode CBOR: %v", err)
		return
	}
	if _, ok := wkRd["di"].(string); !ok {
		err = fmt.Errorf("Cannot find di attribute %v", wkRd)
		return
	}
	if links, ok := wkRd["links"].([]interface{}); !ok || len(links) == 0 {
		err = fmt.Errorf("Cannot find links attribute %v", ok)
		return
	}
	return
}

func sendResponse(s coap.ResponseWriter, client *coap.ClientCommander, code coap.COAPCode, payload []byte) {
	s.SetCode(code)
	if payload != nil {
		s.SetContentFormat(coap.AppCBOR)
	}
	_, err := s.Write(payload)
	if err != nil {
		log.Errorf("Cannot send reply to %v: %v", client.RemoteAddr(), err)
	}
}

func oicRdPostHandler(s coap.ResponseWriter, req *coap.Request) {
	wkRd, err := parsePostPayload(req.Msg)

	if err != nil {
		log.Errorf("%v", err)
		sendResponse(s, req.Client, coap.BadRequest, nil)
		return
	}
	session := clientContainer.find(req.Client.RemoteAddr().String())
	if session == nil {
		log.Errorf("Cannot find session for client %v", req.Client.RemoteAddr())
		sendResponse(s, req.Client, coap.InternalServerError, nil)
		return
	}
	links := wkRd["links"].([]interface{})
	newLinks := make([]interface{}, 0, len(links))
	deviceID := wkRd["di"].(string)
	for i := range links {
		link := links[i].(map[interface{}]interface{})
		obs := false
		if p, ok := link["p"].(map[interface{}]interface{}); ok {
			if bm, ok := p["bm"].(uint64); ok {
				if bm&observable == observable {
					obs = true
				}
			}
		}
		if href, ok := link["href"].(string); ok {
			ins, err := session.publishResource(deviceID, href, obs)
			if err != nil {
				log.Errorf("Cannot publish resource: %v", err)
			} else {
				link["ins"] = ins
				newLinks = append(links, link)
			}
		} else {
			log.Errorf("Cannot find href in link for client %v", req.Client.RemoteAddr())
			sendResponse(s, req.Client, coap.BadRequest, nil)
		}
	}
	wkRd["links"] = newLinks

	var out []byte
	err = codec.NewEncoderBytes(&out, new(codec.CborHandle)).Encode(wkRd)
	if err != nil {
		log.Errorf("Cannot marshal payload for client %v: %v", req.Client.RemoteAddr(), err)
		sendResponse(s, req.Client, coap.InternalServerError, nil)
		return
	}
	sendResponse(s, req.Client, coap.Changed, out)
}

func oicRdDeleteHandler(s coap.ResponseWriter, req *coap.Request) {
	session := clientContainer.find(req.Client.RemoteAddr().String())
	if session == nil {
		log.Errorf("Cannot find session for client %v", req.Client.RemoteAddr())
		sendResponse(s, req.Client, coap.InternalServerError, nil)
		return
	}

	queries := req.Msg.Options(coap.URIQuery)
	var deviceID string
	inss := make(map[int]bool)
	for _, query := range queries {
		q := strings.Split(query.(string), "=")
		if len(q) == 2 {
			switch q[0] {
			case "di":
				deviceID = q[1]
			case "ins":
				i, err := strconv.Atoi(q[1])
				if err != nil {
					log.Errorf("Cannot convert %v to number", q[1])
				}
				inss[i] = true
			}
		}
	}

	err := session.unpublishResource(deviceID, inss)
	if err != nil {
		log.Errorf("%v", err)
	}

	sendResponse(s, req.Client, coap.Deleted, nil)
}

func oicRdHandler(s coap.ResponseWriter, req *coap.Request) {
	switch req.Msg.Code() {
	case coap.POST:
		oicRdPostHandler(s, req)
	case coap.DELETE:
		oicRdDeleteHandler(s, req)
	default:
		log.Errorf("Forbidden request from %v", req.Client.RemoteAddr())
		sendResponse(s, req.Client, coap.Forbidden, nil)
	}
}
