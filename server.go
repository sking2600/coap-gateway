package main

import (
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-ocf/go-coap"
)

//Session a setup of connection
type Session struct {
	server    *Server
	client    coap.Session
	keepalive *Keepalive
}

type ClientContainer struct {
	sessions map[string]*Session
	mutex    sync.Mutex
}

func (c *ClientContainer) addSession(server *Server, client coap.Session) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.sessions[client.LocalAddr().String()] = NewSession(server, client)
}

func (c *ClientContainer) removeSession(s coap.Session) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.sessions[s.LocalAddr().String()].keepalive.Done()
	delete(c.sessions, s.LocalAddr().String())
}

var (
	clientContainer = &ClientContainer{sessions: make(map[string]*Session)}
)

//NewSession create and initialize session
func NewSession(server *Server, client coap.Session) *Session {
	return &Session{server: server, client: client, keepalive: NewKeepalive(server, client)}
}

//Server a configuration of coapgateway
type Server struct {
	// Address to listen on, ":COAP" if empty.
	Addr string
	// if "tcp" or "tcp-tls" (COAP over TLS) it will invoke a TCP listener, otherwise an UDP one
	Net string
	// the duration in seconds between two keepalive transmissions in idle condition. TCP keepalive period is required to be configurable and by default is set to 1 hour.
	keepaliveTime time.Duration
	// the duration in seconds between two successive keepalive retransmissions, if acknowledgement to the previous keepalive transmission is not received.
	keepaliveInterval time.Duration
	// the number of retransmissions to be carried out before declaring that remote end is not available.
	keepaliveRetry int
}

//NewServer setup coap gateway
func NewServer() *Server {
	s := &Server{keepaliveTime: time.Hour, keepaliveInterval: time.Second * 5, keepaliveRetry: 5, Net: "tcp", Addr: "0.0.0.0:5684"}

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
				log.Printf("Invalid value '%v' of env variable '%v: %v'", key, pair[1], err)
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
	return s
}

//NewCoapServer setup coap server
func (server *Server) NewCoapServer() *coap.Server {
	mux := coap.NewServeMux()
	mux.DefaultHandle(coap.HandlerFunc(DefaultHandler))

	return &coap.Server{
		Net:     server.Net,
		Addr:    server.Addr,
		Handler: mux,
		NotifySessionNewFunc: func(s coap.Session) {
			clientContainer.addSession(server, s)
		},
		NotifySessionEndFunc: func(s coap.Session, err error) {
			clientContainer.removeSession(s)
		},
	}
}

//ListenAndServe starts a coapgateway on the configured address in *Server.
func (server *Server) ListenAndServe() error {

	return server.NewCoapServer().ListenAndServe()
}
