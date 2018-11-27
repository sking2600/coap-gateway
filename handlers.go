package main

import (
	"bytes"
	"fmt"
	"log"
	"sync"

	"github.com/go-ocf/go-coap"
	"github.com/ugorji/go/codec"
)

//todo: replace field names with the correct ones from the OCF spec
//todo: I might have to play with the omitempty's
//TODO: figure out usage of a redirect URI
//TODO: finnd a better name than "Account" even though it's an oic.r.account resource
//TODO: coap.message.payload should implement the writer interface
//TODO: make sure that device can't access certain resources unless it's logged in

//this struct kinda covers all payloads besides links. maybe I should break this up more?
type Account struct {
	DeviceID     string `json:"di,omitempty"`
	AuthProvider string `json:"authprovider,omitempty"`
	AccessToken  string `json:"accesstoken"`
	RefreshToken string `json:"refreshtoken,omitempty"`
	TokenTTL     int    `json:"expiresin,omitempty"`
	UserID       string `json:"uid,omitempty"`
	LoggedIn     bool   `json:"login,omitempty"`
}

type ResourcePublication struct {
	DeviceID string `json:"di"`
	Links    []Link `json:"links"`
}

type Link struct {
	Anchor       string     `json:"anchor"`
	Href         string     `json:"href"`
	ResourceType []string   `json:"rt"`
	Interface    []string   `json:"if`
	Policy       Bitmask    `json:"p"` //OCF core spec 7.8.2.1
	Endpoints    []Endpoint `json:"eps"`
}

type Bitmask struct {
	Value uint8 `json:"bm,omitempty"`
	//BIT 0 means discoverable
	//BIT 1 means observable
	//BIT 2-7 are reserved and should be set to 0
	//bm being not included in at all is an acceptable shorthand for bm = 0
}

//Endpoint contains the "ep" field from an OCF link
//when clients try to discover resources on the RD, it may be better to show the pod clusterIP rather than the IP address provided by the device
type Endpoint struct {
	URI string `json:"ep"`
}

type deviceMap struct {
	devices map[string]*coap.ClientCommander
	mutex   sync.Mutex
}

//TODO I think the notifySession funcs should use chans instead of mutex

func (c *deviceMap) addDevice(deviceID string, client *coap.ClientCommander) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.devices[deviceID] = client
}

func (c *deviceMap) removeDevice(deviceID string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.devices, deviceID)
}

func (c *deviceMap) exchange(deviceID string, m coap.Message) (coap.Message, error) {
	return c.devices[deviceID].Exchange(m)
}

var (
	deviceContainer = &deviceMap{devices: make(map[string]*coap.ClientCommander)}
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

//TODO: the coap mux should discriminate between message codes. I should be able to have
//different handlers for UPDATE and DELETE
//TODO: figure out whether it should be POST or PUT for the OCF spec
//todo: check media type for being json, cbor or vnd.ocf+cbor
//maybe also use these mediatypees || mediaType == coap.AppCBOR || coap.AppJSON
//TODO: test error response codes
//POTENTIAL SECURITY VULN: do i need to verify whether this is a mediated token or just an access token in the same field? it seems like a bad idea for the the access token to be able to be used to provision new refresh tokens
func handleAccountUpdateOrDelete(db registry) func(coap.ResponseWriter, *coap.Request) {

	return func(w coap.ResponseWriter, req *coap.Request) {
		if mediaType := req.Msg.Option(coap.ContentFormat).(coap.MediaType); mediaType != coap.AppOcfCbor {
			w.WriteMsg(w.NewResponse(coap.UnsupportedMediaType))
			return
		}
		code := req.Msg.Code()
		if code == coap.PUT || code == coap.POST {
			fmt.Println("code was POST or PUT")
			var a Account
			err := codec.NewDecoderBytes(req.Msg.Payload(), new(codec.CborHandle)).Decode(&a)
			if err != nil {
				err := w.WriteMsg(w.NewResponse(coap.BadRequest))
				if err != nil {
					log.Println("err from writing response about error decoding payload: ", err)
					return
				}
			}
			fmt.Println("decoded vals:\n deviceID: ", a.DeviceID, "\naccessToken: ", a.AccessToken)
			var body Account
			body.AccessToken, body.UserID, body.RefreshToken, body.TokenTTL, err = db.RegisterDevice(a.DeviceID, a.AccessToken)
			if err != nil {
				w.WriteMsg(w.NewResponse(coap.InternalServerError))
				log.Fatal("err registering device: ", err)
			}
			res := w.NewResponse(coap.Created)
			res.SetOption(coap.ContentFormat, coap.AppOcfCbor)

			buf := new(bytes.Buffer)
			h := new(codec.CborHandle)
			h.BasicHandle.Canonical = true
			enc := codec.NewEncoder(buf, h)
			err = enc.Encode(body)
			res.SetPayload(buf.Bytes())
			w.WriteMsg(res)

			if err != nil {
				panic(err)
			}
			//todo add redis device registration
			//todo figure out what the status code and stuff should be for response upon success/failure

		}
		if code == coap.DELETE {
			//TODO delete the user, client or device if the token is valid, return 403/404 otherwise?
		} else {
			w.WriteMsg(w.NewResponse(coap.MethodNotAllowed))
			//TODO send some error about an unsupported code
		}
	}
}

//TODO ensure access token isn't expired
func handleSessionUpdate(db registry) func(coap.ResponseWriter, *coap.Request) {
	return func(w coap.ResponseWriter, req *coap.Request) {
		if req.Msg.Code() != coap.POST {
			w.WriteMsg(w.NewResponse(coap.Unauthorized)) //TODO: double check this is the correct error code
			return
		}
		fmt.Println("code was POST")
		var a Account
		err := codec.NewDecoderBytes(req.Msg.Payload(), new(codec.CborHandle)).Decode(&a)
		if err != nil {
			err := w.WriteMsg(w.NewResponse(coap.BadRequest))
			if err != nil {
				log.Println("err from writing response about error decoding payload: ", err)
				return
			}
		}
		fmt.Println("deviceID: ", a.DeviceID, "\nuserID: ", a.UserID, "\naccessToken: ", a.AccessToken, "\nlogin: ", a.LoggedIn)
		expiresIn, err := db.UpdateSession(a.DeviceID, a.UserID, a.AccessToken, a.LoggedIn)
		if err != nil {
			log.Println("err from registry.UpdateSession: ", err)
			//todo: send internal server error
			return
		}
		if a.LoggedIn {
			b, err := accountToCbor(Account{TokenTTL: expiresIn})
			if err != nil {
				log.Println("error converting struct to cbor when responding to UPDATE /oic/sec/session:\n", err)
				return
			}

			fmt.Println("response payload is ", len(b), " bytes long")
			res := w.NewResponse(coap.Created) //todo: confirm correct response code
			res.SetPayload(b)
			res.SetOption(coap.ContentFormat, coap.AppOcfCbor)
			err = w.WriteMsg(res)
			if err != nil {
				log.Println("error sending token TTL in response to UPDATE /oic/sec/session\n", err)
			}
			deviceContainer.addDevice(a.DeviceID, req.Client)
			return
		}
		//TODO: implement device logging out

	}

}

//TODO this isn't complete
func handleRDUpdate(db registry) func(coap.ResponseWriter, *coap.Request) {
	return func(w coap.ResponseWriter, req *coap.Request) {
		//TODO: make sure device has logged in/has issued an UPDATE request to oic/sec/session
		if mediaType := req.Msg.Option(coap.ContentFormat).(coap.MediaType); mediaType != coap.AppOcfCbor {
			w.WriteMsg(w.NewResponse(coap.UnsupportedMediaType))
			return
		}
		code := req.Msg.Code()
		if code == coap.POST {
			fmt.Println("code was POST or PUT")
			var rp ResourcePublication
			err := codec.NewDecoderBytes(req.Msg.Payload(), new(codec.CborHandle)).Decode(&rp)
			if err != nil {
				err := w.WriteMsg(w.NewResponse(coap.BadRequest))
				if err != nil {
					log.Println("err from writing response about error decoding payload: ", err)
					return
				}
			}
			fmt.Println("decoded vals:\n deviceID: ", rp.DeviceID, "\nLinks: ", rp.Links)

		}

	}
}
func handleTokenRefresh(db registry) func(coap.ResponseWriter, *coap.Request) {
	return func(w coap.ResponseWriter, req *coap.Request) {
		//SELECT user.username, device_uuid,token.refresh_token FROM device INNER JOIN user ON device.user_id = user.user_id INNER JOIN token ON device.token_id = token.token_id;

	}

}

//TODO: this is probably a terrible way of encoding data. the client is recieving {"accesstoken":"","expiresin":-779349} instead of just {expiresin":-779349}
func accountToCbor(a Account) ([]byte, error) {
	buf := new(bytes.Buffer)
	h := new(codec.CborHandle)
	h.BasicHandle.Canonical = true
	enc := codec.NewEncoder(buf, h)
	err := enc.Encode(a)
	return buf.Bytes(), err

}

/*
need to double check these.

UPDATE /oic/sec/account {deviceID, mediated token, authProvider (optional)} returns {access token, userID, refresh token, expires in, redirect URI (optional)}
DELETE /oic/sec/account {access token, userID OR device/clientID}
UPDATE /oic/sec/session {deviceID, userID, loginBool, access token} returns {expires in} <- remember to use VerifyPeerCertificates function
UPDATE /oic/rd {resources (with links and stuff)} returns a "success response" (is that a response code? how do I know the deviceID?)
UPDATE /oic/sec/tokenrefresh {userID, deviceID, refresh token} returns (access token, refresh token, expires in) <- refresh token can be new or old.

*/
