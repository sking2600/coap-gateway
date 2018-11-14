package main

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
	// CPU profiling by default
	//defer profile.Start().Stop()
	// Memory profiling
	//defer profile.Start(profile.MemProfile).Stop()

	//run server
	s, err := NewServer()
	if err != nil {
		log.Fatal(err)
	}
	log.Infof("Server config %v", *s)

	err = s.ListenAndServe()
	if err != nil {
		log.Fatal(err)
	}

}
