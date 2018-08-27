package main

import (
	"crypto/tls"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-ocf/go-coap"
)

func testCreateCoapGateway(t *testing.T) (*coap.Server, string, chan error, error) {

	server, err := NewServer()
	if err != nil {
		return nil, "", nil, err
	}
	var l net.Listener
	switch server.Net {
	case "tcp":
		l, err = net.Listen("tcp", ":")
		if err != nil {
			return nil, "", nil, err
		}
	case "tcp-tls":
		l, err = tls.Listen("tcp", ":", server.TLSConfig)
		if err != nil {
			return nil, "", nil, err
		}
	}

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

func TestSimpleServer(t *testing.T) {
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

	s, err := NewServer()
	if err != nil {
		t.Fatalf("cannot create server: %v", err)
	}
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

func testSetupTLS(t *testing.T, dir string) {
	crt := filepath.Join(dir, "cert.crt")
	if err := ioutil.WriteFile(crt, CertPEMBlock, 0600); err != nil {
		t.Fatalf("%v", err)
	}
	crtKey := filepath.Join(dir, "cert.key")
	if err := ioutil.WriteFile(crtKey, KeyPEMBlock, 0600); err != nil {
		t.Fatalf("%v", err)
	}
	caRootCrt := filepath.Join(dir, "caRoot.crt")
	if err := ioutil.WriteFile(caRootCrt, CARootPemBlock, 0600); err != nil {
		t.Fatalf("%v", err)
	}
	caInterCrt := filepath.Join(dir, "caInter.crt")
	if err := ioutil.WriteFile(caInterCrt, CAIntermediatePemBlock, 0600); err != nil {
		t.Fatalf("%v", err)
	}

	os.Setenv(envListenNet, "tcp-tls")
	os.Setenv(envTLSCertificate, crt)
	os.Setenv(envTLSCertificateKey, crtKey)
	os.Setenv(envTLSCAPool, dir)
}

func TestSetupTLSServer(t *testing.T) {
	dir, err := ioutil.TempDir("", "gotesttmp")
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer os.RemoveAll(dir)

	testSetupTLS(t, dir)

	_, err = NewServer()
	if err != nil {
		t.Fatalf("cannot create server: %v", err)
	}
}

func TestSimpleTLSServer(t *testing.T) {
	dir, err := ioutil.TempDir("", "gotesttmp")
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer os.RemoveAll(dir)

	testSetupTLS(t, dir)
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
	cert, err := tls.X509KeyPair(CertPEMBlock, KeyPEMBlock)
	if err != nil {
		t.Fatalf("unable to build certificate: %v", err)
	}

	c := &coap.Client{Net: "tcp-tls", TLSConfig: &tls.Config{
		InsecureSkipVerify: true,
		Certificates:       []tls.Certificate{cert},
	}}
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

var (
	CertPEMBlock = []byte(`-----BEGIN CERTIFICATE-----
MIIBkzCCATegAwIBAgIUF399tsbWkMnMF6NWt6j/MbUIZvUwDAYIKoZIzj0EAwIF
ADARMQ8wDQYDVQQDEwZSb290Q0EwHhcNMTgwNzAyMDUzODQwWhcNMjgwNzAyMDUz
ODQwWjA0MTIwMAYDVQQDEyl1dWlkOjYxNTVmMjFjLTA3MjItNDZjOC05ZDcxLTMw
NGE1NTMyNzllOTBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABBTvmtgfe49ZY0L0
B7wC/XH5V1jJ3NFdLyPZZFmz9O731JB7dwGYVUtaRai5cPM349mIw9k5kX8Zww7E
wMf4jw2jSDBGMAkGA1UdEwQCMAAwDgYDVR0PAQH/BAQDAgGIMCkGA1UdJQQiMCAG
CCsGAQUFBwMBBggrBgEFBQcDAgYKKwYBBAGC3nwBBjAMBggqhkjOPQQDAgUAA0gA
MEUCIBPNUqmjeTFIMkT3Y1qqUnR/fQmqbhxR8gScBsz8m3w8AiEAlH3Nf57vFqqh
tuvff9aSBdNlDBlQ5dTLu24V7fScLLI=
-----END CERTIFICATE-----`)

	KeyPEMBlock = []byte(`-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIGqPsr+N0x/CBmykEGm04TXvsykwxwqAy32SpVO2ANB0oAoGCCqGSM49
AwEHoUQDQgAEFO+a2B97j1ljQvQHvAL9cflXWMnc0V0vI9lkWbP07vfUkHt3AZhV
S1pFqLlw8zfj2YjD2TmRfxnDDsTAx/iPDQ==
-----END EC PRIVATE KEY-----`)

	CARootPemBlock = []byte(`-----BEGIN CERTIFICATE-----
MIIBazCCAQ+gAwIBAgIUY9HA4Of2KwJm5HaP72+VkLpUCpYwDAYIKoZIzj0EAwIF
ADARMQ8wDQYDVQQDEwZSb290Q0EwHhcNMTgwNjIyMTEyMzM1WhcNMjgwNjIyMTEy
MzM1WjARMQ8wDQYDVQQDEwZSb290Q0EwWTATBgcqhkjOPQIBBggqhkjOPQMBBwNC
AAREWwFfs+rAjPZ80alM/dQEWFOILkpkkwadCGomdiEBwLdlJEKGHomcVNJ39xBV
nte6BA4fOP7a9kdrsbRe/qKao0MwQTAMBgNVHRMEBTADAQH/MA4GA1UdDwEB/wQE
AwIBBjAhBgNVHSUEGjAYBgorBgEEAYLefAEGBgorBgEEAYLefAEHMAwGCCqGSM49
BAMCBQADSAAwRQIgI95uRXx5y4iehqKq1CP99agqlPGc8JaMMIzvwn5lYBICIQC8
KokSEk+DVrYiWUubIxl/tSCtwC8jyA2jKO7CY63cQg==
-----END CERTIFICATE-----
`)

	CAIntermediatePemBlock = []byte(`-----BEGIN CERTIFICATE-----
MIIBdzCCARqgAwIBAgIUMFZsksJ1spFMlONPi+v0EkDcD+EwDAYIKoZIzj0EAwIF
ADARMQ8wDQYDVQQDEwZSb290Q0EwHhcNMTgwNjIyMTEyNDMwWhcNMjgwNjIyMTEy
NDMwWjAZMRcwFQYDVQQDEw5JbnRlcm1lZGlhdGVDQTBZMBMGByqGSM49AgEGCCqG
SM49AwEHA0IABBRR8WmmkmVWvFvdi1YyanKOV3FOiMwZ1blfAOnfUhWjBv2AVLJG
bRZ/fo+7BF8peD/BYQkbs1KAkH/nxnDeQLyjRjBEMA8GA1UdEwQIMAYBAf8CAQAw
DgYDVR0PAQH/BAQDAgEGMCEGA1UdJQQaMBgGCisGAQQBgt58AQYGCisGAQQBgt58
AQcwDAYIKoZIzj0EAwIFAANJADBGAiEA8VNPyaUzaIUOsqdvoaT3dCZDBbLjOx8R
XVqB37LdYPcCIQDiqvcbW0aOfVcvMDVs3r1HavgKuTIHgJ9uzSOAAF17vg==
-----END CERTIFICATE-----
`)
)
