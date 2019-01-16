package registry

import (
	"context"
	"errors"
	"net/url"
)

//TODO figure out how to properly communicate to the client that the device is registered, but not connected (using ::/128 address?)
//TODO how do i enforce uniqueness of tokens? should I enforce that uniqueness?

var (
	errorMediatorTokenNotfound = errors.New("mediator token not found")
	unspecifiedAddress         = "::/128"
	tokenEntropy               = 32   //the actual tokens will be longer due to base64 encoding
	accessTokenTTL             = 6000 //TTL is seconds. TODO: make this configurable
)

type Registry interface {
	RegisterUser(username, authProvider string) (string, error)
	ProvisionMediator(username, token string) (string, error)
	ProvisionDevice(ctx context.Context, deviceUUID, mediatorToken string) (string, error)
	RegisterDevice(deviceUUID, mediatedToken string) (accessToken, userID, refreshToken string, expiresIn int, err error)
	DeleteDevice(deviceID, accessToken string) error
	ProvisionClient(ctx context.Context, clientUUID, mediatorToken string) (string, error)
	//redirectURI is optional so you should always check if redirectURI == ""
	RegisterClient(ctx context.Context, userID, clientUUID, mediatedToken, authProvider string) (accessToken, refreshToken, redirectURI string, expiresIn int, err error)
	DeleteClient(ctx context.Context, clientID, accessToken string) error
	UpdateSession(deviceID, userID, accessToken, podAddr string, loggedIn bool) (int, error)
	RefreshToken(deviceID, userID, refreshToken string) (accessToken string, optionallyNewRefreshToken string, ttl int, err error)
	//LookupPrivateIP looks up the IP of the pod that's connected to the device with that UUID
	//I should probably change this method name
	LookupPrivateIP(deviceUUID string) (string, error)

	//PublishResource handles the db side of POST /oic/rd {deviceID, []Link}
	PublishResource(json, deviceID string) error
	//FindDevice takes the parameters from a GET /oic/res request and returns the published resources matching all params
	//TODO figure out exactly which params to support, and then write the db queries
	//for the time being, only support querying by device UUID? maybe resource types?
	//TODO use the url.Values type from net/url instead of string. look into url.ParseQuery()
	FindDevice(userID string, params url.Values) (publishedResources string, err error)
}
