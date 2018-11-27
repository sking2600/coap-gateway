package main

import (
	"bytes"
	"fmt"
	"log"

	coap "github.com/go-ocf/go-coap"
	"github.com/ugorji/go/codec"
)

type Account struct {
	DeviceID     string `json:"di,omitempty"`
	AuthProvider string `json:"authprovider,omitempty"`
	AccessToken  string `json:"accesstoken"`
	RefreshToken string `json:"refreshtoken,omitempty"`
	TokenTTL     int    `json:"expiresin,omitempty"`
	UserID       string `json:"uid,omitempty"`
	LoggedIn     bool   `json:"login,omitempty"`
}

func main() {

	fmt.Println("dialing server")
	client := coap.Client{Net: "tcp", Handler: coap.HandlerFunc(logHandler)} //DialTimeout: 10 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, SyncTimeout: 10 * time.Second}
	conn, err := client.Dial("localhost:5684")
	if err != nil {
		log.Fatal("err dialing localhost: ", err)
	}
	fmt.Println("dialed server")
	body := Account{DeviceID: "device-test-uuid", AccessToken: "mlKXF9cocYaREIV6hGk7C9Db6ZHX0zvdW2DViUH7uMM=", UserID: "MW1VqsF1oPLIKw==", LoggedIn: true}
	buf := new(bytes.Buffer)
	h := new(codec.CborHandle)
	h.BasicHandle.Canonical = true
	enc := codec.NewEncoder(buf, h)
	err = enc.Encode(body)
	msg, err := conn.NewPostRequest("oic/sec/session", coap.AppOcfCbor, buf)
	if err != nil {
		fmt.Println("err from creating newPostRequest: ", err)
		return
	}
	fmt.Println("about to exchange")
	m, err := conn.Exchange(msg)
	if err != nil {
		fmt.Println("error from exchanging message: ", err)
	}
	fmt.Println("exchanged")
	fmt.Println("payload is ", len(m.Payload()), " bytes long")
	decodeMsg(m, "message recieved from server")
	select {}
}

func decodeMsg(resp coap.Message, tag string) {
	var m interface{}
	log.Printf("-------------------%v-----------------\n", tag)
	log.Printf("Path: %v\n", resp.PathString())
	err := codec.NewDecoderBytes(resp.Payload(), new(codec.CborHandle)).Decode(&m)
	if err != nil {
		log.Printf("RAW:\n%v\n", resp.Payload())
	} else {
		bw := new(bytes.Buffer)
		h := new(codec.JsonHandle)
		h.BasicHandle.Canonical = true
		enc := codec.NewEncoder(bw, h)
		err = enc.Encode(m)
		if err != nil {
			panic(err)
		}
		log.Printf("JSON:\n%v\n", bw.String())
	}
}

func logHandler(w coap.ResponseWriter, r *coap.Request) {
	//decodeMsg(r.Msg, "request from cloud")
	fmt.Println("request from cloud:\n", "payloadLength: ", len(r.Msg.Payload()[:]), "\npayload: ", string(r.Msg.Payload()[:]), "\nhref: ", r.Msg.PathString())
	w.WriteMsg(w.NewResponse(coap.Valid))
}

/*

func main() {



	fmt.Println("dialing server")
	client := coap.Client{Net: "tcp", Handler: coap.HandlerFunc(logHandler)} //DialTimeout: 10 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, SyncTimeout: 10 * time.Second}
	conn, err := client.Dial("localhost:5684")
	if err != nil {
		log.Fatal("err dialing localhost: ", err)
	}
	fmt.Println("dialed server")
	body := Account{DeviceID: "device-test-uuid", AccessToken: "ZSLEFPfywQxaHi8Ce410eCYLWZRP6zLyhuMUUTUmUtU="}
	buf := new(bytes.Buffer)
	h := new(codec.CborHandle)
	h.BasicHandle.Canonical = true
	enc := codec.NewEncoder(buf, h)
	err = enc.Encode(body)
	msg, err := conn.NewPostRequest("oic/sec/account", coap.AppOcfCbor, buf)
	if err != nil {
		fmt.Println("err from creating newPostRequest: ", err)
		return
	}
	fmt.Println("about to exchange")
	m, err := conn.Exchange(msg)
	if err != nil {
		fmt.Println("error from exchanging message: ", err)
	}
	fmt.Println("exchanged")

	decodeMsg(m, "message recieved from server")
}
*/
