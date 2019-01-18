package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-ocf/go-coap"
	"github.com/sking2600/coap-gateway/pkg/registry"

	"github.com/go-redis/redis"
	"github.com/go-zoo/bone"
	"github.com/kelseyhightower/envconfig"
)

//TODO: coap lib examples use coap.dial instead of client.dial
//TODO: do I need to worry about congestion control stuff? (RFC 7252 sec 4.2, 4.7-4.8)

type dbconfig struct {
	dbName        string `envconfig:"DB_NAME required:"true"`
	dbUsername    string `envconfig:"DB_USERNAME" required:"true"`
	dbPassword    string `envconfig:"DB_PASSWORD" required:"true"`
	dbAddress     string `envconfig:"DB_URI" required:"true"`
	redisPassword string `envconfig:"CACHE_PASSWORD" required:"true"`
	redisAddress  string `envconfig:"CACHE_URI" required:"true"`
	redisNumber   int    `envconfig:"CACHE_NUMBER" default:0"`
}

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

	tokenEntropy   = 32   //the actual tokens will be longer due to base64 encoding
	accessTokenTTL = 6000 //TTL is seconds. TODO: make this configurable

	podAddr = os.Getenv("MY_POD_IP") //TODO I should probably have a default value like "missing pod IP" to make debugging easier

)

func main() {
	var dbc dbconfig
	err := envconfig.Process("db", &dbc)
	err = envconfig.Process("cache", &dbc)
	if err != nil {
		log.Fatal(err.Error())
	}
	dbURI := fmt.Sprintf("%s:%s%s%s?parseTime=true", dbc.dbUsername, dbc.dbPassword, dbc.dbAddress, dbc.dbName)
	if podAddr == "" {
		log.Println("no pod IP provided in env. setting podAddr to 'localhost'")
		podAddr = "localhost"

	}
	//fmt.Println(dbURI)
	db, err := sql.Open("mysql", dbURI)
	if err != nil {
		log.Fatal(err)
	}
	err = db.Ping()
	if err != nil {
		panic(fmt.Sprint("error from pinging mysql: ", err))
	}
	redisdb := redis.NewClient(&redis.Options{
		Addr:     dbc.redisAddress,
		Password: dbc.redisPassword,
		DB:       0,
	})
	fmt.Println("created registry")
	reg := registry.MysqlRedisRegistry{db, redisdb}
	s, err := NewServer(reg)
	if err != nil {
		log.Fatal("err from register device: ", err)
	}
	//coapServer := s.NewCoapServer()
	fmt.Println("starting server")
	router := bone.New()
	router.Get("/", http.HandlerFunc(handleHealthCheck))
	router.Get("/healthz", http.HandlerFunc(handleHealthCheck))
	router.Post("/:deviceUUID/:href", http.HandlerFunc(handleClientRequest))
	fmt.Println("started server")
	go func() { log.Fatal(http.ListenAndServe(":8081", router)) }()

	go func() { log.Fatal(s.ListenAndServe()) }()
	select {}

}

/*
	accessToken, err := reg.ProvisionDevice(context.TODO(), "device-test-uuid", "2F1W5fnjK1anvsSir6tgLx5h8-pPZzJOaOHFlYi-bSQ=")
	if err != nil {
		log.Fatal("err from provisionDevice: ", err, "\naccess token: ", accessToken)
	}
	accessToken, userID, refreshToken, ttl, err := reg.RegisterDevice("device-test-uuid", accessToken)
	fmt.Println("access token: ", accessToken, "\nuserID: ", userID, "\nrefresheToken: ", refreshToken, "\nttl: ", ttl, "\nerr: ", err)
	//s, err := NewServer(reg)
	if err != nil {
		log.Fatal("err from register device: ", err)
	}
	//	log.Fatal(s.ListenAndServe())
*/

func ticker() {
	ticker := time.NewTicker(5 * time.Second)
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				if len(deviceContainer.devices) > 0 {
					for deviceID, conn := range deviceContainer.devices {
						req, _ := conn.NewPostRequest("/myResource", coap.AppOcfCbor, bytes.NewBufferString(deviceID))
						conn.Exchange(req)
					}
				}
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()
	fmt.Println("started ticker")
}

//TODO: handle authZ with the access tokens
//TODO convert content format to coap.AppOcfCbor if it's a different format like coap.AppJSON
func handleClientRequest(w http.ResponseWriter, r *http.Request) {
	deviceUUID := bone.GetValue(r, "deviceUUID")
	href := bone.GetValue(r, "href")
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Println("error parsing request body", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("STATUS CODE 500:\ncouldn't read body"))
	}
	if _, ok := deviceContainer.devices[deviceUUID]; !ok {
		log.Println("client made request to deviceUUID == ", deviceUUID, " but it was not found")
		w.WriteHeader(http.StatusNotFound)
		//TODO is this the correct status code?
		return
	}
	log.Println("client requested to send to: ", deviceUUID, "\nand this href: ", href, "\nand this body:", string(b[:]))
	req, err := deviceContainer.devices[deviceUUID].NewPostRequest(href, coap.AppJSON, bytes.NewBuffer(b))
	if err != nil {
		log.Println("error creating coap POST request: ", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	res, err := deviceContainer.devices[deviceUUID].Exchange(req)
	if err != nil {
		log.Println("error exchanging message with deviceUUID:", deviceUUID, ": ", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	log.Println("response from exchanging message with device: ", string(res.Payload()))
	w.Write(res.Payload())

}

func handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	log.Println("get a health check request")
	w.WriteHeader(200)
}
