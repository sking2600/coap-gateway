package main

import (
	"log"
	"time"

	"github.com/go-ocf/go-coap"
)

//Keepalive setup of keepalive
type Keepalive struct {
	time     time.Duration
	interval time.Duration
	retry    int
	client   *coap.ClientCommander

	doneChan chan interface{}
}

//Done wake and end goroutine
func (k *Keepalive) Done() {
	var v interface{}
	k.doneChan <- v
}

//Terminate terminate connection by keepalive
func (k *Keepalive) Terminate() {
	log.Printf("Terminate connection %v by keepalive %v", k.client.RemoteAddr(), k)
	k.client.Close()
}

func (k *Keepalive) run() {
	timeoutCount := 0
	for {
		waitTime := k.time
		if timeoutCount > 0 {
			waitTime = k.interval
		}
		select {
		case <-k.doneChan:
			return
		case <-time.After(time.Second * waitTime):
			if err := k.client.Ping(time.Second); err != nil {
				log.Printf("Cannot send PING to %v: %v", k.client.RemoteAddr(), err)
				if err == coap.ErrTimeout {
					timeoutCount++
					if timeoutCount >= k.retry {
						k.Terminate()
						return
					}
				} else {
					//other error then timeout - connection was closed
					return
				}

			}
			timeoutCount = 0
		}
	}
}

//NewKeepalive create new Keepalive instance and start check of connection
func NewKeepalive(server *Server, client *coap.ClientCommander) *Keepalive {
	k := &Keepalive{
		time:     server.keepaliveTime,
		interval: server.keepaliveInterval,
		retry:    server.keepaliveRetry,
		client:   client,
		doneChan: make(chan interface{}, 1),
	}
	go k.run()
	return k
}
