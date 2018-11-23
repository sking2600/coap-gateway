package main

import (
	"sync"

	coap "github.com/go-ocf/go-coap"
)

type ClientContainer struct {
	sessions map[string]*Session
	mutex    sync.Mutex
}

func (c *ClientContainer) add(server *Server, client *coap.ClientCommander) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.sessions[client.RemoteAddr().String()] = newSession(server, client)
}

func (c *ClientContainer) find(remoteAddr string) *Session {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if session, ok := c.sessions[remoteAddr]; ok {
		return session
	}
	return nil
}

func (c *ClientContainer) remove(s *coap.ClientCommander) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.sessions[s.RemoteAddr().String()].close()
	delete(c.sessions, s.RemoteAddr().String())
}
