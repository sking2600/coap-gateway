package main

import (
	"log"
)

//constants
var (
	envKeepaliveTime     = "KEEPALIVE_TIME"
	envKeepaliveInterval = "KEEPALIVE_INTERVAL"
	envKeepaliveRetry    = "KEEPALIVE_RETRY"
	envListenAddress     = "ADDRESS"
	envListenNet         = "NETWORK"
	envTLSCertificate    = "TLS_CERTIFICATE"
	envTLSCertificateKey = "TLS_CERTIFICATE_KEY"
	envTLSCAPool         = "TLS_CA_POOL"
)

func main() {

	//run server
	s, err := NewServer()
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(s.ListenAndServe())
}
