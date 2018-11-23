package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-ocf/go-coap"
)

//Server a configuration of coapgateway
type Server struct {
	Addr              string        // Address to listen on, ":COAP" if empty.
	Net               string        // if "tcp" or "tcp-tls" (COAP over TLS) it will invoke a TCP listener, otherwise an UDP one
	TLSConfig         *tls.Config   // TLS connection configuration
	keepaliveTime     time.Duration // the duration in seconds between two keepalive transmissions in idle condition. TCP keepalive period is required to be configurable and by default is set to 1 hour.
	keepaliveInterval time.Duration // the duration in seconds between two successive keepalive retransmissions, if acknowledgement to the previous keepalive transmission is not received.
	keepaliveRetry    int           // the number of retransmissions to be carried out before declaring that remote end is not available.

	clientContainer *ClientContainer
}

func setupTLS() (*tls.Config, error) {
	var tlsCertificate *string
	var tlsCertificateKey *string
	var tlsCAPool *string
	for _, e := range os.Environ() {
		pair := strings.Split(e, "=")
		key := pair[0]
		switch key {
		case envTLSCertificate:
			tlsCertificate = &pair[1]
		case envTLSCertificateKey:
			tlsCertificateKey = &pair[1]
		case envTLSCAPool:
			tlsCAPool = &pair[1]
		}
	}
	if tlsCertificate == nil {
		return nil, ErrEnvNotSet(envTLSCertificate)
	}
	if tlsCertificateKey == nil {
		return nil, ErrEnvNotSet(envTLSCertificateKey)
	}
	if tlsCAPool == nil {
		return nil, ErrEnvNotSet(envTLSCAPool)
	}
	cert, err := tls.LoadX509KeyPair(*tlsCertificate, *tlsCertificateKey)
	if err != nil {
		return nil, err
	}

	caRootPool := x509.NewCertPool()
	caIntermediatesPool := x509.NewCertPool()

	err = filepath.Walk(*tlsCAPool, func(path string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}

		// check if it is a regular file (not dir)
		if info.Mode().IsRegular() {
			certPEMBlock, err := ioutil.ReadFile(path)
			if err != nil {
				log.Errorf("Cannot read file '%v': %v", path, err)
				return nil
			}
			certDERBlock, _ := pem.Decode(certPEMBlock)
			if certDERBlock == nil {
				log.Errorf("Cannot decode der block '%v'", path)
				return nil
			}
			if certDERBlock.Type != "CERTIFICATE" {
				log.Errorf("DER block is not certificate '%v'", path)
				return nil
			}
			caCert, err := x509.ParseCertificate(certDERBlock.Bytes)
			if err != nil {
				log.Errorf("Cannot parse certificate '%v': %v", path, err)
				return nil
			}
			if bytes.Compare(caCert.RawIssuer, caCert.RawSubject) == 0 && caCert.IsCA {
				log.Infof("Adding root certificate '%v'", path)
				caRootPool.AddCert(caCert)
			} else if caCert.IsCA {
				log.Infof("Adding intermediate certificate '%v'", path)
				caIntermediatesPool.AddCert(caCert)
			} else {
				log.Warnf("Ignoring certificate '%v'", path)
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	if len(caRootPool.Subjects()) == 0 {
		return nil, ErrEmptyCARootPool
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAnyClientCert,
		VerifyPeerCertificate: func(rawCerts [][]byte, verifyChains [][]*x509.Certificate) error {
			for _, rawCert := range rawCerts {
				cert, err := x509.ParseCertificates(rawCert)
				if err != nil {
					return err
				}
				//TODO verify revocation
				for _, c := range cert {
					_, err := c.Verify(x509.VerifyOptions{
						Intermediates: caIntermediatesPool,
						Roots:         caRootPool,
						CurrentTime:   time.Now(),
						KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
					})
					if err != nil {
						return err
					}
				}
				//TODO verify EKU - need to use ASN decoding
			}
			return nil
		},
	}, nil
}

//NewServer setup coap gateway
func NewServer() (*Server, error) {
	s := Server{keepaliveTime: time.Hour, keepaliveInterval: time.Second * 5, keepaliveRetry: 5, Net: "tcp", Addr: "0.0.0.0:5684", clientContainer: &ClientContainer{sessions: make(map[string]*Session)}}

	//load env variables
	var keepaliveTime *int
	var keepaliveInterval *int
	var keepaliveRetry *int
	var listenNetwork *string
	var listenAddress *string
	for _, e := range os.Environ() {
		pair := strings.Split(e, "=")
		key := pair[0]
		switch key {
		case envKeepaliveTime, envKeepaliveInterval, envKeepaliveRetry:
			val, err := strconv.Atoi(pair[1])
			if err != nil {
				log.Errorf("Invalid value '%v' of env variable '%v: %v'", key, pair[1], err)
			}
			switch key {
			case envKeepaliveTime:
				keepaliveTime = &val
			case envKeepaliveInterval:
				keepaliveInterval = &val
			case envKeepaliveRetry:
				keepaliveRetry = &val
			}
		case envListenAddress:
			listenAddress = &pair[1]
		case envListenNet:
			listenNetwork = &pair[1]
		}
	}

	if listenNetwork != nil {
		s.Net = *listenNetwork
	}
	if listenAddress != nil {
		s.Addr = *listenAddress
	}
	if keepaliveTime != nil {
		s.keepaliveTime = time.Duration(*keepaliveTime)
	}
	if keepaliveInterval != nil {
		s.keepaliveInterval = time.Duration(*keepaliveInterval)
	}
	if keepaliveRetry != nil {
		s.keepaliveRetry = *keepaliveRetry
	}
	if strings.Contains(s.Net, "tls") {
		var err error
		s.TLSConfig, err = setupTLS()
		if err != nil {
			return nil, err
		}
	}

	return &s, nil
}

func validateCommandCode(s coap.ResponseWriter, req *coap.Request, server *Server, fnc func(s coap.ResponseWriter, req *coap.Request, server *Server)) {
	decodeMsgToDebug(req.Msg, "MESSAGE_FROM_CLIENT")
	switch req.Msg.Code() {
	case coap.POST, coap.DELETE, coap.PUT, coap.GET:
		fnc(s, req, server)
	case coap.Content:
		log.Infof("Unpaired message received from %v", req.Client.RemoteAddr())
	default:
		log.Errorf("Invalid code received %v from %v", req.Msg.Code(), req.Client.RemoteAddr())
	}
}

//NewCoapServer setup coap server
func (server *Server) NewCoapServer() *coap.Server {
	mux := coap.NewServeMux()
	mux.DefaultHandle(coap.HandlerFunc(func(s coap.ResponseWriter, req *coap.Request) {
		validateCommandCode(s, req, server, defaultHandler)
	}))
	mux.Handle(oicRd, coap.HandlerFunc(func(s coap.ResponseWriter, req *coap.Request) {
		validateCommandCode(s, req, server, oicRdHandler)
	}))
	mux.Handle(oicSecAccount, coap.HandlerFunc(func(s coap.ResponseWriter, req *coap.Request) {
		validateCommandCode(s, req, server, oicSecAccountHandler)
	}))
	mux.Handle(oicSecSession, coap.HandlerFunc(func(s coap.ResponseWriter, req *coap.Request) {
		validateCommandCode(s, req, server, oicSecSessionHandler)
	}))

	return &coap.Server{
		Net:       server.Net,
		Addr:      server.Addr,
		TLSConfig: server.TLSConfig,
		Handler:   mux,
		NotifySessionNewFunc: func(s *coap.ClientCommander) {
			server.clientContainer.add(server, s)
		},
		NotifySessionEndFunc: func(s *coap.ClientCommander, err error) {
			server.clientContainer.remove(s)
		},
	}
}

//ListenAndServe starts a coapgateway on the configured address in *Server.
func (server *Server) ListenAndServe() error {
	return server.NewCoapServer().ListenAndServe()
}
