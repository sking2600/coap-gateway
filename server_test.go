package main

import (
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-ocf/go-coap"
)

func testCreateCoapGateway(t *testing.T) (*coap.Server, string, chan error, error) {
	l, err := net.Listen("tcp", ":")
	if err != nil {
		return nil, "", nil, err
	}
	server := NewServer()

	coapserver := server.NewCoapServer()
	coapserver.Listener = l
	waitLock := sync.Mutex{}
	coapserver.NotifyStartedFunc = waitLock.Unlock
	waitLock.Lock()

	// See the comment in RunLocalUDPServerWithFinChan as to
	// why fin must be buffered.
	fin := make(chan error, 1)

	go func() {
		fin <- coapserver.ActivateAndServe()
		l.Close()
	}()

	waitLock.Lock()
	return coapserver, l.Addr().String(), fin, nil
}

func TestServer(t *testing.T) {
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

	c := &coap.Client{Net: "tcp"}
	co, err := c.Dial(addrstr)
	if err != nil {
		t.Fatalf("unable to dialing: %v", err)
	}
	defer co.Close()

	token, err := coap.GenerateToken(8)
	if err != nil {
		t.Fatalf("cannot create token: %v", err)
	}

	req := co.NewMessage(coap.MessageParams{
		Type:  coap.NonConfirmable,
		Code:  coap.GET,
		Token: token,
	})
	req.SetPathString("/test")

	resp, err := co.Exchange(req, time.Second)
	if err != nil {
		t.Fatalf("cannot exchange messages: %v", err)
	}

	if resp.Code() != coap.NotFound {
		t.Fatalf("unexpected message %v", resp)
	}
}

func TestShutdownClient(t *testing.T) {
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

	c := &coap.Client{Net: "tcp"}
	co, err := c.Dial(addrstr)
	if err != nil {
		t.Fatalf("unable to dialing: %v", err)
	}
	co.Close()
}

func TestSetupServer(t *testing.T) {
	keepaliveTime := 10000
	keepaliveRetry := 10001
	keepaliveInterval := 10002
	address := "a"
	network := "n"
	os.Setenv(envKeepaliveTime, strconv.Itoa(keepaliveTime))
	os.Setenv(envKeepaliveInterval, strconv.Itoa(keepaliveInterval))
	os.Setenv(envKeepaliveRetry, strconv.Itoa(keepaliveRetry))
	os.Setenv(envListenAddress, address)
	os.Setenv(envListenNet, network)

	s := NewServer()
	if s.keepaliveTime != time.Duration(keepaliveTime) {
		t.Fatalf("invalid keepaliveTime: %v != %v ", s.keepaliveTime, keepaliveTime)
	}
	if s.keepaliveInterval != time.Duration(keepaliveInterval) {
		t.Fatalf("invalid keepaliveInterval: %v != %v ", s.keepaliveInterval, keepaliveInterval)
	}
	if s.keepaliveRetry != keepaliveRetry {
		t.Fatalf("invalid keepaliveRetry: %v != %v ", s.keepaliveRetry, keepaliveRetry)
	}
	if s.Addr != address {
		t.Fatalf("invalid address: %v != %v ", s.Addr, address)
	}
	if s.Net != network {
		t.Fatalf("invalid network: %v != %v ", s.Net, network)
	}
}
