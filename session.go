package main

import (
	"fmt"
	"reflect"
	"sync"

	coap "github.com/go-ocf/go-coap"
)

type publishedResource struct {
	id          int
	observation *coap.Observation
}

//Session a setup of connection
type Session struct {
	server    *Server
	client    *coap.ClientCommander
	keepalive *Keepalive

	lockPublishedResources sync.Mutex
	publishedResources     map[string]map[string]publishedResource
	publishedResourcesID   int
}

//NewSession create and initialize session
func newSession(server *Server, client *coap.ClientCommander) *Session {
	log.Infof("Close session %v", client.RemoteAddr())
	return &Session{
		server:             server,
		client:             client,
		keepalive:          NewKeepalive(server, client),
		publishedResources: make(map[string]map[string]publishedResource),
	}
}

func (session *Session) publishResource(deviceID, href string, observable bool) (int, error) {
	session.lockPublishedResources.Lock()
	defer session.lockPublishedResources.Unlock()
	if _, ok := session.publishedResources[deviceID]; !ok {
		session.publishedResources[deviceID] = make(map[string]publishedResource)
	}
	if _, ok := session.publishedResources[deviceID][href]; ok {
		return -1, fmt.Errorf("Resource ocf://%v/%v are already published", deviceID, href)
	}
	return session.addPublishedResourceLocked(deviceID, href, observable), nil
}

func (session *Session) addPublishedResourceLocked(deviceID, href string, observable bool) int {
	var observation *coap.Observation
	log.Infof("add published resource ocf://%v/%v, observable: %v", deviceID, href, observable)
	if observable {
		obs, err := session.client.Observe(href, onObserveNotification)
		if err != nil {
			log.Errorf("Cannot observe ocf://%v/%v", deviceID, href)
		} else {
			observation = obs
		}
	} else {
		go func(client *coap.ClientCommander, deviceId string, href string) {
			resp, err := client.Get(href)
			if err != nil {
				log.Errorf("Cannot get ocf://%v/%v", deviceId, href)
				return
			}
			onGetResponse(&coap.Request{Client: client, Msg: resp})
		}(session.client, deviceID, href)
	}
	publishedResourcesID := session.publishedResourcesID
	session.publishedResourcesID++
	session.publishedResources[deviceID][href] = publishedResource{id: publishedResourcesID, observation: observation}
	return publishedResourcesID
}

func (session *Session) removePublishedResourceLocked(deviceID, href string) error {
	log.Infof("remove published resource ocf://%v/%v", deviceID, href)
	obs := session.publishedResources[deviceID][href].observation
	if obs != nil {
		log.Infof("cancel observation of ocf://%v/%v", deviceID, href)
		err := obs.Cancel()
		if err != nil {
			log.Errorf("Cannot cancel observation ocf//%v/%v", deviceID, href)
		}
	}

	delete(session.publishedResources[deviceID], href)
	if len(session.publishedResources[deviceID]) == 0 {
		delete(session.publishedResources, deviceID)
	}
	return nil
}

func (session *Session) unpublishResource(deviceID string, publishedResourcesIDs map[int]bool) error {
	session.lockPublishedResources.Lock()
	defer session.lockPublishedResources.Unlock()

	if hrefs, ok := session.publishedResources[deviceID]; ok {
		if len(publishedResourcesIDs) == 0 {
			for href := range hrefs {
				session.removePublishedResourceLocked(deviceID, href)
			}
			return nil
		}
		for href, pubRsx := range hrefs {
			if _, ok := publishedResourcesIDs[pubRsx.id]; ok {
				session.removePublishedResourceLocked(deviceID, href)
				delete(publishedResourcesIDs, pubRsx.id)
			}
		}
		if len(publishedResourcesIDs) == 0 {
			return nil
		}
		out := make([]int, 0, len(publishedResourcesIDs))
		for _, val := range reflect.ValueOf(publishedResourcesIDs).MapKeys() {
			out = append(out, val.Interface().(int))
		}
		return fmt.Errorf("Cannot unpublish resources with %v: resource not found", out)
	}
	return fmt.Errorf("Cannot unpublish resource for %v: device not found", deviceID)

}

func (session *Session) close() {
	log.Infof("Close session %v", session.client.RemoteAddr())
	session.keepalive.Done()
	session.lockPublishedResources.Lock()
	defer session.lockPublishedResources.Unlock()
	for deviceID, hrefs := range session.publishedResources {
		for href := range hrefs {
			err := session.removePublishedResourceLocked(deviceID, href)
			if err != nil {
				log.Errorf("Cannot remove published resource ocf//%v/%v", deviceID, href)
			}
		}
	}
}
