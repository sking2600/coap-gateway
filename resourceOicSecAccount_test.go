package main

import (
	"testing"

	coap "github.com/go-ocf/go-coap"
)

func TestOicSecAccountPostHandler(t *testing.T) {
	tbl := []testEl{
		{"Changed", input{coap.POST, `{}`, nil}, output{coap.Changed, `{"accesstoken":"abc","uid":"00000000-0000-0000-0000-000000000000"}`, nil}},
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

	for _, test := range tbl {
		tf := func(t *testing.T) {
			testPostHandler(t, oicSecAccount, test, co)
		}
		t.Run(test.name, tf)
	}
}
