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
	queries []string
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
	if len(json) == 0 {
		return "", nil
	}
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

var tblOicRd = []testEl{
	{"BadRequest", input{coap.POST, `{ "di":"a" }`, nil}, output{coap.BadRequest, ``, nil}},
	{"BadRequest", input{coap.POST, `{ "di":"a", "links":"abc" }`, nil}, output{coap.BadRequest, ``, nil}},
	{"BadRequest", input{coap.POST, `{ "di":"a", "links":[ "abc" ]}`, nil}, output{coap.BadRequest, ``, nil}},
	{"BadRequest", input{coap.POST, `{ "di":"a", "links":[ {} ]}`, nil}, output{coap.BadRequest, ``, nil}},
	{"Changed", input{coap.POST, `{ "di":"a", "links":[ { "href":"/a" } ]}`, nil}, output{coap.Changed, `{ "di":"a", "links":[ { "href":"/a", "ins":0 } ]}`, nil}},
	{"Changed", input{coap.POST, `{ "di":"a", "links":[ { "href":"/b" } ]}`, nil}, output{coap.Changed, `{ "di":"a", "links":[ { "href":"/b", "ins":1 } ]}`, nil}},
	{"BadRequest", input{coap.POST, `{ "di":"a", "links":[ { "href":"" } ]}`, nil}, output{coap.BadRequest, ``, nil}},
	{"Changed", input{coap.POST, `{ "di":"b", "links":[ { "href":"/c", "p": {"bm":2} } ]}`, nil}, output{coap.Changed, `{ "di":"b", "links":[ { "href":"/c", "ins":2, "p": {"bm":2} } ]}`, nil}},
}

func testPostHandler(t *testing.T, path string, test testEl, co *coap.ClientConn) {
	inputCbor, err := json2cbor(test.in.payload)
	if err != nil {
		t.Fatalf("Cannot convert json to cbor: %v", err)
	}

	req, err := co.NewPostRequest(path, coap.AppCBOR, bytes.NewReader(inputCbor))
	if err != nil {
		t.Fatalf("cannot create request: %v", err)
	}
	for _, q := range test.in.queries {
		req.AddOption(coap.URIQuery, q)
	}

	resp, err := co.Exchange(req)
	if err != nil {
		if err != nil {
			t.Fatalf("Cannot send/retrieve msg: %v", err)
		}
	}

	if resp.Code() != test.out.code {
		t.Fatalf("Ouput code %v is invalid, expected %v", resp.Code(), test.out.code)
	} else {
		if len(resp.Payload()) > 0 || len(test.out.payload) > 0 {
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
		if len(test.out.queries) > 0 {
			queries := resp.Options(coap.URIQuery)
			if resp == nil {
				t.Fatalf("Output doesn't contains queries, expected: %v", test.out.queries)
			}
			if len(queries) == len(test.out.queries) {
				t.Fatalf("Invalid queries %v, expected: %v", queries, test.out.queries)
			}
			for idx := range queries {
				if queries[idx] != test.out.queries[idx] {
					t.Fatalf("Invalid query %v, expected %v", queries[idx], test.out.queries[idx])
				}
			}
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

	for _, test := range tblOicRd {
		tf := func(t *testing.T) {
			testPostHandler(t, oicRd, test, co)
		}
		t.Run(test.name, tf)
	}
}

func TestOicRdDeleteHandler(t *testing.T) {
	deletetblOicRd := []testEl{
		{"NotExist", input{coap.DELETE, ``, []string{"xxx"}}, output{coap.BadRequest, ``, nil}},
		{"Exist1", input{coap.DELETE, ``, []string{"di=a"}}, output{coap.Deleted, ``, nil}},
		{"Exist2", input{coap.DELETE, ``, []string{"di=b", "ins=5"}}, output{coap.BadRequest, ``, nil}},
		{"Exist3", input{coap.DELETE, ``, []string{"di=b", "ins=2"}}, output{coap.Deleted, ``, nil}},
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
	for _, test := range tblOicRd {
		testPostHandler(t, oicRd, test, co)
	}

	//delete resources
	for _, test := range deletetblOicRd {
		tf := func(t *testing.T) {
			req, err := co.NewDeleteRequest(oicRd)
			if err != nil {
				t.Fatalf("cannot create request: %v", err)
			}
			for _, q := range test.in.queries {
				req.AddOption(coap.URIQuery, q)
			}

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
