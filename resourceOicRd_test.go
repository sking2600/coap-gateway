package main

import (
	"bytes"
	"testing"

	coap "github.com/go-ocf/go-coap"
	"github.com/ugorji/go/codec"
)

type input struct {
	code    coap.COAPCode
	payload string
	path    string
}

type output input

func json2cbor(json string) ([]byte, error) {
	var data interface{}
	err := codec.NewDecoderBytes([]byte(json), new(codec.JsonHandle)).Decode(&data)
	if err != nil {
		return nil, err
	}
	var out []byte
	return out, codec.NewEncoderBytes(&out, new(codec.CborHandle)).Encode(data)
}

func cannonalizeJSON(json string) (string, error) {
	var data interface{}
	err := codec.NewDecoderBytes([]byte(json), new(codec.JsonHandle)).Decode(&data)
	if err != nil {
		return "", err
	}
	var out []byte
	h := codec.JsonHandle{}
	h.BasicHandle.Canonical = true
	err = codec.NewEncoderBytes(&out, &h).Encode(data)
	return string(out), err
}

func cbor2json(cbor []byte) (string, error) {
	var data interface{}
	err := codec.NewDecoderBytes(cbor, new(codec.CborHandle)).Decode(&data)
	if err != nil {
		return "", err
	}
	var out []byte
	h := codec.JsonHandle{}
	h.BasicHandle.Canonical = true
	err = codec.NewEncoderBytes(&out, &h).Encode(data)
	return string(out), err
}

type testEl struct {
	name string
	in   input
	out  output
}

var tbl = []testEl{
	{"BadRequest", input{coap.POST, `{ "di":"a" }`, "a"}, output{coap.BadRequest, ``, "a"}},
	{"BadRequest", input{coap.POST, `{ "di":"a", "links":"abc" }`, "a"}, output{coap.BadRequest, ``, "a"}},
	{"BadRequest", input{coap.POST, `{ "di":"a", "links":[ "abc" ]}`, "a"}, output{coap.BadRequest, ``, "a"}},
	{"BadRequest", input{coap.POST, `{ "di":"a", "links":[ {} ]}`, "a"}, output{coap.BadRequest, ``, "a"}},
	{"Changed", input{coap.POST, `{ "di":"a", "links":[ { "href":"/a" } ]}`, "a"}, output{coap.Changed, `{ "di":"a", "links":[ { "href":"/a", "ins":0 } ]}`, "a"}},
	{"Changed", input{coap.POST, `{ "di":"a", "links":[ { "href":"/b" } ]}`, "a"}, output{coap.Changed, `{ "di":"a", "links":[ { "href":"/b", "ins":1 } ]}`, "a"}},
	{"BadRequest", input{coap.POST, `{ "di":"a", "links":[ { "href":"" } ]}`, "a"}, output{coap.BadRequest, ``, "a"}},
	{"Changed", input{coap.POST, `{ "di":"a", "links":[ { "href":"/c", "p": {"bm":2} } ]}`, "a"}, output{coap.Changed, `{ "di":"a", "links":[ { "href":"/c", "ins":2, "p": {"bm":2} } ]}`, "a"}},
}

func testOicRdPostHandler(t *testing.T, test testEl, co *coap.ClientConn) {
	inputCbor, err := json2cbor(test.in.payload)
	if err != nil {
		t.Fatalf("Cannot convert json to cbor: %v", err)
	}

	resp, err := co.Post(oicrd, coap.AppCBOR, bytes.NewReader(inputCbor))
	if err != nil {
		if err != nil {
			t.Fatalf("Cannot send/retrieve msg: %v", err)
		}
	}

	if resp.Code() != test.out.code {
		t.Fatalf("Ouput code %v is invalid, expected %v", resp.Code(), test.out.code)
	} else if len(resp.Payload()) > 0 || len(test.out.payload) > 0 {
		json, err := cbor2json(resp.Payload())
		if err != nil {
			t.Fatalf("Cannot convert cbor to json: %v", err)
		}
		expJSON, err := cannonalizeJSON(test.out.payload)
		if err != nil {
			t.Fatalf("Cannot convert cbor to json: %v", err)
		}
		if json != expJSON {
			t.Fatalf("Ouput payload %v is invalid, expected %v", json, expJSON)
		}
	}
}

func TestOicRdPostHandler(t *testing.T) {

	s, addrstr, fin, err := testCreateCoapGateway(t)
	if err != nil {
		t.Fatalf("unable to run test server: %v", err)
	}
	defer func() {
		s.Shutdown()
		err := <-fin
		if err != nil {
			t.Fatalf("server unexcpected shutdown: %v", err)
		}
	}()

	client := &coap.Client{Net: "tcp"}
	co, err := client.Dial(addrstr)
	if err != nil {
		t.Fatalf("unable to dialing: %v", err)
	}
	defer co.Close()

	for _, test := range tbl {
		tf := func(t *testing.T) {
			testOicRdPostHandler(t, test, co)
		}
		t.Run(test.name, tf)
	}
}

func TestOicRdDeleteHandler(t *testing.T) {
	deleteTbl := []testEl{
		{"NotExist", input{coap.DELETE, ``, "xxx"}, output{coap.BadRequest, ``, "xxx"}},
		{"Exist", input{coap.DELETE, ``, "a"}, output{coap.Deleted, ``, "a"}},
	}

	s, addrstr, fin, err := testCreateCoapGateway(t)
	if err != nil {
		t.Fatalf("unable to run test server: %v", err)
	}
	defer func() {
		s.Shutdown()
		err := <-fin
		if err != nil {
			t.Fatalf("server unexcpected shutdown: %v", err)
		}
	}()

	client := &coap.Client{Net: "tcp"}
	co, err := client.Dial(addrstr)
	if err != nil {
		t.Fatalf("unable to dialing: %v", err)
	}
	defer co.Close()

	//publish resources
	for _, test := range tbl {
		testOicRdPostHandler(t, test, co)
	}

	//delete resources
	for _, test := range deleteTbl {
		tf := func(t *testing.T) {
			req, err := co.NewDeleteRequest(oicrd)
			if err != nil {
				t.Fatalf("cannot create request: %v", err)
			}
			query := "di=" + test.in.path
			req.AddOption(coap.URIQuery, query)

			resp, err := co.Exchange(req)
			if err != nil {
				if err != nil {
					t.Fatalf("Cannot send/retrieve msg: %v", err)
				}
			}

			if resp.Code() != test.out.code {
				t.Fatalf("Ouput code %v is invalid, expected %v", resp.Code(), test.out.code)
			} else if len(resp.Payload()) > 0 || len(test.out.payload) > 0 {
				json, err := cbor2json(resp.Payload())
				if err != nil {
					t.Fatalf("Cannot convert cbor to json: %v", err)
				}
				expJSON, err := cannonalizeJSON(test.out.payload)
				if err != nil {
					t.Fatalf("Cannot convert cbor to json: %v", err)
				}
				if json != expJSON {
					t.Fatalf("Ouput payload %v is invalid, expected %v", json, expJSON)
				}
			}
		}
		t.Run(test.name, tf)
	}
}
