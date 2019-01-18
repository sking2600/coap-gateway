package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"github.com/kelseyhightower/envconfig"

	"github.com/go-redis/redis"
	"github.com/go-zoo/bone"

	"github.com/sking2600/coap-gateway/pkg/registry"
)

//TODO: break up main and handlers

/*

note: bearer token for oic/res requests should be in the authorization header
no need for oic/sec/session for HTTP since there's no long lived session

new user registration using OAuth (currently in gothicTest.go)
POST /provision/mediator {user token, permissions.json} returns mediator token
POST /oic/sec/tokenrefresh {userID, deviceID, refresh token} returns (access token, refresh token, expires in) <- refresh token can be new or old.
POST /provision/client {mediator token, deviceID} returns {mediated token}
POST /provision/device {mediator token, deviceID} returns {mediated token}
GET /oic/res?param1=A&param2=B {access token in authorization header}
POST {device-UUID}/{device-specific href} {TODO: should payload be CBOR or JSON encoded? I could just store that info in the relevant header}
DELETE /oic/sec/account {access token, userID OR device/clientID}
POST /oic/sec/account {deviceID, access token, authProvider (optional)} returns {access token, userID, refresh token, expires in, redirect URI (optional)}

maybe GET /shadow/{device-UUID}/ in order to get the device shadow?
*/
/*
const (
	redisAddress  = "redis-15564.c1.us-central1-2.gce.cloud.redislabs.com:15564"
	redisPassword = "ocfftw"
)
*/

//TODO enforce JSON encoding for provisioning/registration requests

type Account struct {
	DeviceID     string `json:"di,omitempty"`
	AuthProvider string `json:"authprovider,omitempty"`
	AccessToken  string `json:"accesstoken,omitempty"`
	RefreshToken string `json:"refreshtoken,omitempty"`
	TokenTTL     int    `json:"expiresin,omitempty"`
	UserID       string `json:"uid,omitempty"`
	LoggedIn     bool   `json:"login,omitempty"`
}

type dbconfig struct {
	dbName        string `envconfig:"DB_NAME required:"true"`
	dbUsername    string `envconfig:"DB_USERNAME" required:"true"`
	dbPassword    string `envconfig:"DB_PASSWORD" required:"true"`
	dbAddress     string `envconfig:"DB_URI" required:"true"`
	redisPassword string `envconfig:"CACHE_PASSWORD" required:"true"`
	redisAddress  string `envconfig:"CACHE_URI" required:"true"`
	redisNumber   int    `envconfig:"CACHE_NUMBER" default:0"`
}

var (
	tokenEntropy   int = 32   //the actual tokens will be longer due to base64 encoding
	accessTokenTTL     = 6000 //TTL is seconds. TODO: make this configurable

)

func main() {
	var dbc dbconfig
	err := envconfig.Process("db", &dbc)
	err = envconfig.Process("cache", &dbc)
	if err != nil {
		log.Fatal(err.Error())
	}
	dbURI := fmt.Sprintf("%s:%s%s%s?parseTime=true", dbc.dbUsername, dbc.dbPassword, dbc.dbAddress, dbc.dbName)

	fmt.Println(dbURI)
	sql, err := sql.Open("mysql", dbURI)
	if err != nil {
		log.Fatal(err)
	}
	redisdb := redis.NewClient(&redis.Options{
		Addr:     dbc.redisAddress,
		Password: dbc.redisPassword,
		DB:       dbc.redisNumber,
	})
	db := registry.MysqlRedisRegistry{sql, redisdb}
	db.Ping()
	fmt.Println("db connection successful")
	router := bone.New()
	router.Post("/register/user", http.HandlerFunc(handleRegisterUser(db)))
	router.Post("/provision/mediator", http.HandlerFunc(provisionMediator(db)))
	router.Post("/oic/sec/tokenrefresh", http.HandlerFunc(tokenRefresh(db)))
	router.Post("/provision/client", http.HandlerFunc(handleProvisionClient(db)))
	router.Post("/provision/device", http.HandlerFunc(handleProvisionDevice(db)))
	router.Post("/:deviceUUID/:href", http.HandlerFunc(handleClientRequest(db)))
	router.Delete("/oic/sec/account", http.HandlerFunc(handleDelete))
	router.Post("/oic/sec/account", http.HandlerFunc(handleRegisterClient(db)))
	router.Get("/oic/res", http.HandlerFunc(handleResourceDiscovery))
	_, err = registry.InitDB(context.TODO(), sql)
	if err != nil {
		panic(err)
	}
	log.Fatal(http.ListenAndServe(":8080", router))
}

//TODO implement this properly once the TG agrees on auth
func handleRegisterUser(db registry.Registry) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Println("err from handleRegisterUser: ", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		var account Account
		err = json.Unmarshal(user, &account)
		if err != nil {
			log.Println("err from handleRegisterUser: ", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		userToken, err := db.RegisterUser(account.UserID, account.AuthProvider)
		if err != nil {
			log.Println("err from handleRegisterUser: ", err)
			w.WriteHeader(http.StatusInternalServerError) //TODO parse the error and return different status code if it's caused by a duplicate entry
		}
		payload, err := json.Marshal(Account{AccessToken: userToken})
		if err != nil {
			log.Println("err from handleRegisterUser: ", err)
		}
		w.Write(payload)

	}

}

//TODO implement this properly once the TG agrees on auth
//TODO parse and save the JSON permissions
//TODO should I be putting the token in the header or the body?
func provisionMediator(db registry.Registry) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Println("err from provisionMediator: ", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		var account Account
		err = json.Unmarshal(user, &account)
		if err != nil {
			log.Println("err from provisionMediator: ", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		mediatorToken, err := db.ProvisionMediator(account.UserID, account.AccessToken)
		if err != nil {
			log.Println("err from provisionMediator: ", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		response, err := json.Marshal(Account{AccessToken: mediatorToken})
		if err != nil {
			log.Println("err from provisionMediator: ", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		w.Write(response)
	}
}

func tokenRefresh(db registry.Registry) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var account Account
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Println("err from tokenRefresh: ", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		err = json.Unmarshal(body, &account)
		if err != nil {
			log.Println("err from tokenRefresh json.unmarshal: ", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		accessToken, refreshToken, ttl, err := db.RefreshToken(account.DeviceID, account.UserID, account.RefreshToken)
		if err != nil {
			log.Println("err from tokenRefresh: ", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		response, err := json.Marshal(Account{AccessToken: accessToken, RefreshToken: refreshToken, TokenTTL: ttl})
		if err != nil {
			log.Println("err from tokenRefresh: ", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		w.Write(response)
	}
}

//TODO the client UUID probably shouldn't be kept within the "di" field but the OCF spec doesn't give specific guidance
func handleProvisionClient(db registry.Registry) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		mediatorToken := r.Header.Get("Authorization")
		if strings.Contains(mediatorToken, "Bearer") {
			mediatorToken = strings.Split(mediatorToken, "Bearer ")[1]
		}
		var account Account
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Println("error retrieving request body: ", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		err = json.Unmarshal(body, &account)
		if err != nil {
			log.Println("error parsing request body: ", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Println("client provision request with mediator token: ", mediatorToken, " for uuid: ", account.DeviceID)

		mediatedToken, err := db.ProvisionClient(context.TODO(), account.DeviceID, mediatorToken)
		if err != nil {
			log.Println("error provisioning client: ", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		response, err := json.Marshal(Account{AccessToken: mediatedToken})
		if err != nil {
			log.Println("error marshalling response body: ", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write(response)

	}

}

//todo implement parsing stuff properly
func handleProvisionDevice(db registry.Registry) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		mediatorToken := r.Header.Get("Authorization")
		if strings.Contains(mediatorToken, "Bearer") {
			mediatorToken = strings.Split(mediatorToken, "Bearer ")[1]
		}
		var account Account
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Println("error retrieving request body: ", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		err = json.Unmarshal(body, &account)
		if err != nil {
			log.Println("error parsing request body: ", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		fmt.Println("device provision request with mediator token: ", mediatorToken, " for uuid: ", account.DeviceID)

		//TODO verify token with auth provider
		mediatedToken, err := db.ProvisionDevice(context.TODO(), account.DeviceID, mediatorToken)
		if err != nil {
			log.Println("err from provisioning device: ", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		response, err := json.Marshal(Account{AccessToken: mediatedToken})
		if err != nil {
			log.Println("error marshalling response body: ", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		w.Write(response)
	}
}

func handleClientRequest(db registry.Registry) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		//todo: verify access token in relation to deviceUUID
		deviceUUID := bone.GetValue(r, "deviceUUID")
		href := bone.GetValue(r, "href")
		b, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Println("error parsing request body", err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("STATUS CODE 500:\ncouldn't read body"))
		}
		ip, err := db.LookupPrivateIP(deviceUUID)
		if err != nil {
			if err == redis.Nil {
				log.Println("client requested deviceUUID: ", deviceUUID, " but it was not found")
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte("that deviceUUID was not found. it may not be connected or it may have never been registered"))
			}
		}
		ip = strings.Replace(ip, ".", "-", -1)
		fmt.Println("client requested to send to ", deviceUUID, " at ip address ", ip, "\nand this href: ", href, " and this body:\n", string(b))
		//TODO implement use of k8s dns by making the url 1-2-3-4.default.pod.cluster.local {replace 1-2-3-4 with podIP but gotta change the dots to dashes}
		//TODO what is the best way of making this app aware of the namespace of the coap pod?
		endpoint := fmt.Sprintf("http://%s.default.pod.cluster.local:8081/%s/%s", ip, deviceUUID, href)
		res, err := http.Post(endpoint, "application/json", bytes.NewBuffer(b))
		if err != nil {
			log.Println("err sending request to coap gateway: ", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		response, err := ioutil.ReadAll(res.Body)
		fmt.Println("len of coap-gateway response: ", len(response))

		if err != nil {
			log.Println("err reading response from coap gateway: ", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		fmt.Println("response from coap-gateway: ", string(response))
		w.Write(response) //TODO convert payload from cbor to json. maybe this is best done on the coap-gateway side?
	}
}

//TODO implement this
func handleDelete(w http.ResponseWriter, r *http.Request) {

}

func handleRegisterClient(db registry.Registry) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		var account Account
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Println("err from reading body from POST /oic/sec/account: ", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		err = json.Unmarshal(body, &account)
		if err != nil {
			log.Println("err from unmarshalling body from POST /oic/sec/account: ", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		//TODO I should check for empty fields
		if account.UserID == "" || account.DeviceID == "" || account.AccessToken == "" {
			log.Println("mandatory fields were left unpopulated")
			w.WriteHeader(http.StatusUnauthorized) //TODO ensure this is the correct response code
		}
		log.Println("in handleMediatedToken accessToken: ", account.AccessToken)
		accessToken, refreshToken, redirectURI, expiresIn, err := db.RegisterClient(context.TODO(), account.UserID, account.DeviceID, account.AccessToken, account.AuthProvider)
		if err != nil {
			log.Println("err from registering client: ", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if redirectURI != "" {
			log.Panic("non-nil redirectURI was returend by RegisterClient. this feature is not yet supported")
		}
		//todo should I support redirectURI?
		log.Println(fmt.Sprintf("[debug] values from handleMediatedToken:\nuserID:%s\ndeviceID:%s\naccessToken:%s\n", account.UserID, account.DeviceID, account.AccessToken))
		account = Account{UserID: account.UserID, AccessToken: accessToken, RefreshToken: refreshToken, TokenTTL: expiresIn}
		response, err := json.Marshal(account)
		if err != nil {
			log.Println("err from marshalling response to POST /oic/sec/account: ", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		w.Write(response)

	}
}

//select * from mediator where mediator_token like '%%'
// ^^ this is a good jumping off point for handling parameters that aren't specified
//TODO implement this
func handleResourceDiscovery(w http.ResponseWriter, r *http.Request) {
	//get token from auth header
	err := r.ParseForm()
	if err != nil {
		log.Println("err parsing form from handleResourceDirectory: ", err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	//TODO figure out request formatting required in order to correctly/efficiently identify userID based on token?

}
