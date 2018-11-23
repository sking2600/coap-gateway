package main

import (
	"fmt"
	"strconv"
	"strings"

	coap "github.com/go-ocf/go-coap"
	"github.com/ugorji/go/codec"
)

const observable = 2

var oicrd = "oic/rd"

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

func processLink(l interface{}, session *Session, deviceID string) (link map[interface{}]interface{}, err error) {
	link, ok := l.(map[interface{}]interface{})
	if !ok {
		err = fmt.Errorf("Unsupporterd type of link")
		return
	}
	obs := false
	if p, ok := link["p"].(map[interface{}]interface{}); ok {
		if bm, ok := p["bm"].(uint64); ok {
			if bm&observable == observable {
				obs = true
			}
		}
	}
	if href, ok := link["href"].(string); ok && len(href) > 0 {
		ins, err := session.publishResource(deviceID, href, obs)
		if err != nil {
			err = fmt.Errorf("Cannot publish resource: %v", err)
			return link, err
		}
		link["ins"] = ins
		return link, err
	}
	err = fmt.Errorf("Cannot find href in link")
	return
}

func oicRdPostHandler(s coap.ResponseWriter, req *coap.Request, server *Server) {
	wkRd, err := parsePostPayload(req.Msg)

	if err != nil {
		log.Errorf("%v", err)
		sendResponse(s, req.Client, coap.BadRequest, nil)
		return
	}
	session := server.clientContainer.find(req.Client.RemoteAddr().String())
	if session == nil {
		log.Errorf("Cannot find session for client %v", req.Client.RemoteAddr())
		sendResponse(s, req.Client, coap.InternalServerError, nil)
		return
	}
	links := wkRd["links"].([]interface{})
	newLinks := make([]interface{}, 0, len(links))
	deviceID := wkRd["di"].(string)
	for i := range links {
		link, err := processLink(links[i], session, deviceID)
		if err != nil {
			log.Errorf("Cannot process link from %v: %v", req.Client.RemoteAddr(), err)
			sendResponse(s, req.Client, coap.BadRequest, nil)
			return
		}
		newLinks = append(newLinks, link)
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

func oicRdDeleteHandler(s coap.ResponseWriter, req *coap.Request, server *Server) {
	session := server.clientContainer.find(req.Client.RemoteAddr().String())
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
		sendResponse(s, req.Client, coap.BadRequest, nil)
		return
	}

	sendResponse(s, req.Client, coap.Deleted, nil)
}

func oicRdHandler(s coap.ResponseWriter, req *coap.Request, server *Server) {
	switch req.Msg.Code() {
	case coap.POST:
		oicRdPostHandler(s, req, server)
	case coap.DELETE:
		oicRdDeleteHandler(s, req, server)
	default:
		log.Errorf("Forbidden request from %v", req.Client.RemoteAddr())
		sendResponse(s, req.Client, coap.Forbidden, nil)
	}
}
