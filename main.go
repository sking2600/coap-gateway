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

	"github.com/go-redis/redis"
	"github.com/go-zoo/bone"
)

//TODO: coap lib examples use coap.dial instead of client.dial
//TODO: do I need to worry about congestion control stuff? (RFC 7252 sec 4.2, 4.7-4.8)

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

	dbName         = os.Getenv("DB_NAME")
	dbUsername     = os.Getenv("DB_USERNAME")
	dbPassword     = os.Getenv("DB_PASSWORD")
	dbAddress      = os.Getenv("DB_URI")
	dbParameters   = os.Getenv("DB_PARAMETERS")
	tokenEntropy   = 32   //the actual tokens will be longer due to base64 encoding
	accessTokenTTL = 6000 //TTL is seconds. TODO: make this configurable

	podAddr       = os.Getenv("MY_POD_IP") //TODO I should probably have a default value like "missing pod IP" to make debugging easier
	redisPassword = os.Getenv("CACHE_PASSWORD")
	redisAddress  = os.Getenv("CACHE_URI")
)

func main() {

	//run server
	dbURI := fmt.Sprintf("%s:%s%s%s%s", dbUsername, dbPassword, dbAddress, dbName, dbParameters)
	fmt.Println(dbURI)
	db, err := sql.Open("mysql", dbURI)
	if err != nil {
		log.Fatal(err)
	}
	redisdb := redis.NewClient(&redis.Options{
		Addr:     redisAddress,
		Password: redisPassword,
		DB:       0,
	})
	fmt.Println("created databases")
	reg := MysqlRedisRegistry{db, redisdb}
	s, err := NewServer(reg)
	if err != nil {
		log.Fatal("err from register device: ", err)
	}
	//coapServer := s.NewCoapServer()
	fmt.Println("starting server")
	router := bone.New()
	router.Post("/:deviceUUID/:href", http.HandlerFunc(handleClientRequest))
	go func() { log.Fatal(http.ListenAndServe(":8080", router)) }()

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

//TODO: handle authZ with the tokens
func handleClientRequest(w http.ResponseWriter, r *http.Request) {
	deviceUUID := bone.GetValue(r, "deviceUUID")
	href := bone.GetValue(r, "href")
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Println("error parsing request body", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("STATUS CODE 500:\ncouldn't read body"))
	}

	fmt.Println("client requested to send to ", deviceUUID, "\nand this href: ", href, "\nand this body:", string(b[:]))
	req, _ := deviceContainer.devices[deviceUUID].NewPostRequest(href, coap.AppJSON, bytes.NewBuffer(b))
	res, _ := deviceContainer.devices[deviceUUID].Exchange(req)
	w.Write(res.Payload())

}
