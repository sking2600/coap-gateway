package main

import (
	"bytes"
	"fmt"
	"log"
	"strings"

	coap "github.com/go-ocf/go-coap"
	"github.com/ugorji/go/codec"
)

//TODO the UUID field is char(36) when it should be char(37) based on the char count from the OCF spec
var (
	resourcePublish = `[{
		"di": "aaaa-bbbb-cccc-dddd-eeee-1234567890a",
		"links": [{
			"anchor": "ocf://aaaa-bbbb-cccc-dddd-eeee-1234567890ab",
			"href": "/myLightSwitch",
			"rt": ["oic.r.switch.binary"],
			"if": ["oic.if.a", "oic.if.baseline"],
			"p": {
				"bm": 3
			},
			"eps": [{
				"ep": "coaps://[fe80::b1d6]:22222"
			}]
		}]
	}]`
	resourceArray = `[{
		"di": "aaaa-bbbb-cccc-dddd-eeee-1234567890ab",
		"links": [{
			"anchor": "ocf://aaaa-bbbb-cccc-dddd-eeee-1234567890ab",
			"href": "/myLightSwitch",
			"rt": ["oic.r.switch.binary"],
			"if": ["oic.if.a", "oic.if.baseline"],
			"p": {
				"bm": 3
			},
			"eps": [{
				"ep": "coaps://[fe80::b1d6]:22222"
			}]
		}]
	},
	{
		"di": "QQQQ-bbbb-cccc-dddd-eeee-1234567890ab",
		"links": [{
			"anchor": "ocf://aaaa-bbbb-cccc-dddd-eeee-1234567890ab",
			"href": "/yourLightSwitch",
			"rt": ["oic.r.switch.binary"],
			"if": ["oic.if.a", "oic.if.baseline"],
			"p": {
				"bm": 3
			},
			"eps": [{
				"ep": "coaps://[fe80::b1d6]:22222"
			}]
		}]
	}
]`
	myLink = Link{Anchor: "ocf://test-this-uuid-dddd-eeee-1234567890ab",
		Href:         "myHref",
		ResourceType: []string{"oic.r.switch.binary"},
		Interface:    []string{"oic.if.a", "oic.if.baseline"},
		Policy:       Bitmask{Value: 3},
		Endpoints:    []Endpoint{Endpoint{URI: "coaps://[fe80::b1d6]:22222"}},
	}
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
type ResourcePublication struct {
	DeviceID string `json:"di"`
	Links    []Link `json:"links"`
}

type Link struct {
	Anchor       string     `json:"anchor"`
	Href         string     `json:"href"`
	ResourceType []string   `json:"rt"`
	Interface    []string   `json:"if"`
	Policy       Bitmask    `json:"p"` //OCF core spec 7.8.2.1
	Endpoints    []Endpoint `json:"eps"`
}

type Bitmask struct {
	Value uint8 `json:"bm,omitempty"`
	//BIT 0 means discoverable
	//BIT 1 means observable
	//BIT 2-7 are reserved and should be set to 0
	//bm being not included in at all is an acceptable shorthand for bm = 0
}

//Endpoint contains the "ep" field from an OCF link
//when clients try to discover resources on the RD, it may be better to show the pod clusterIP rather than the IP address provided by the device
type Endpoint struct {
	URI string `json:"ep"`
}

func main() {
	var address string
	var userID string
	var deviceID string
	var accessToken string
	var mediatedToken string
	var refreshToken string
	/*
		if len(os.Args) == 5 {
			address = os.Args[1]
			if address == "" {

				address = "localhost"
			}
			userID = os.Args[2]
			if userID == "" {
				panic("userID is nil")
			}
			deviceID = os.Args[3]
			if deviceID == "" {
				panic("deviceID is nil")
			}
			token := os.Args[4]
			if token == "" {
				panic("token is nil")
			}
		}
	*/
	//(TODO get those env vars from stdin only as a fallback?)

	fmt.Println("what is your target address?")
	fmt.Scanln(&address)
	fmt.Println("what is the user ID?: ")
	fmt.Scanln(&userID)
	fmt.Println("what is the device ID?: ")
	fmt.Scanln(&deviceID)
	fmt.Println("what is the access token?: ")
	fmt.Scanln(&accessToken)
	fmt.Println("what is the mediatedToken? (aka one-time-use access token)")
	fmt.Scanln(&mediatedToken)
	fmt.Println("what is the refresh token?")
	fmt.Scanln(&refreshToken)

	fmt.Println("dialing server")
	client := coap.Client{Net: "tcp", Handler: coap.HandlerFunc(logHandler)} //DialTimeout: 10 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, SyncTimeout: 10 * time.Second}
	if !strings.Contains(address, ":") {
		address = fmt.Sprint(address, ":5684")
	}
	conn, err := client.Dial(address)
	if err != nil {
		log.Fatal("err dialing ", address, ": ", err)
	}
	fmt.Println("dialed server")
	go promptUser(deviceID, accessToken, userID, conn)
	select {}
}

func promptUser(deviceID, accessToken, userID string, conn *coap.ClientConn) {
	fmt.Println("what do you want to do? You can register with the server using a mediated token, establish a session or publish resources (non-configurable payload)\nOptions: register, session, publish")
	var input string
	fmt.Scanln(&input)
	switch input {
	case "register":
		registerDevice(deviceID, accessToken, userID, true, conn)
	case "session":
		startSession(deviceID, accessToken, userID, true, conn)
	case "publish":
		publishResource(deviceID, accessToken, userID, conn)
	default:
		fmt.Println("invalid input. please type \"register\" or \"session\" or \"publish\"")

	}
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
	log.Println("request from cloud:\n", "payloadLength: ", len(r.Msg.Payload()[:]), "\npayload: ", string(r.Msg.Payload()[:]), "\nhref: ", r.Msg.PathString())
	if len(r.Msg.Payload()[:]) == 0 {
		panic("response payload is 0. this is prob an infinite loop")
	}
	res := w.NewResponse(coap.Valid)
	res.SetPayload([]byte("test response payload from device"))
	res.SetOption(coap.ContentFormat, coap.AppOcfCbor)
	err := w.WriteMsg(res)
	if err != nil {
		log.Println("err sending response from device to http server: ", err)
	}
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

//MarshalCBOR marshals an account struct into a binary CBOR payload
//TODO: this is probably a terrible way of encoding data. the client is recieving {"accesstoken":"","expiresin":-779349} instead of just {expiresin":-779349}
func (a Account) MarshalCBOR() ([]byte, error) {
	buf := new(bytes.Buffer)
	h := new(codec.CborHandle)
	h.BasicHandle.Canonical = true
	enc := codec.NewEncoder(buf, h)
	err := enc.Encode(a)
	return buf.Bytes(), err

}

//UnmarshalCBOR unmarshals a CBOR payload into an account struct
func UnmarshalCBOR(b []byte) (Account, error) {
	var a Account
	err := codec.NewDecoderBytes(b, new(codec.CborHandle)).Decode(&a)
	if err != nil {
		return Account{}, err
	}
	return a, err
}

func registerDevice(deviceID, accessToken, userID string, loggedIn bool, conn *coap.ClientConn) {
	fmt.Println("in registerDevice stub")
	body, err := Account{DeviceID: deviceID, AccessToken: accessToken, UserID: userID, LoggedIn: true}.MarshalCBOR()
	if err != nil {
		log.Println("error marshalling session request to CBOR: ", err)
	}
	msg, err := conn.NewPostRequest("oic/sec/account", coap.AppOcfCbor, bytes.NewBuffer(body))
	if err != nil {
		fmt.Println("err from creating newPostRequest: ", err)
		return
	}
	fmt.Println("about to exchange with oic/sec/account")
	//not trying to test this handler right now
	m, err := conn.Exchange(msg)
	if err != nil {
		fmt.Println("error from exchanging message: ", err)
	}
	fmt.Println("exchanged")

	fmt.Println("payload is ", len(m.Payload()), " bytes long")
	decodeMsg(m, "response from POST oic/sec/account")
	promptUser(deviceID, accessToken, userID, conn)
}

func startSession(deviceID, accessToken, userID string, loggedIn bool, conn *coap.ClientConn) {
	//fmt.Println("do you want to log in or out? (respond with 'in' or 'out'")

	body, err := Account{DeviceID: deviceID, AccessToken: accessToken, UserID: userID, LoggedIn: loggedIn}.MarshalCBOR()
	msg, err := conn.NewPostRequest("oic/sec/session", coap.AppOcfCbor, bytes.NewBuffer(body))
	fmt.Println("about to exchange with oic/sec/session")
	m, err := conn.Exchange(msg)
	if err != nil {
		fmt.Println("err from exchanging message to initiate session: ", err)
	}
	fmt.Println("exchanged")

	fmt.Println("payload is ", len(m.Payload()), " bytes long")
	decodeMsg(m, "response from POST oic/sec/account")
	promptUser(deviceID, accessToken, userID, conn)
}

func publishResource(deviceID, accessToken, userID string, conn *coap.ClientConn) {
	fmt.Println("in publishResource stub")
	resource := ResourcePublication{DeviceID: deviceID, Links: []Link{myLink}}
	buf := new(bytes.Buffer)
	h := new(codec.CborHandle)
	h.BasicHandle.Canonical = true
	enc := codec.NewEncoder(buf, h)
	err := enc.Encode(resource)
	msg, err := conn.NewPostRequest("oic/rd", coap.AppOcfCbor, buf)
	if err != nil {
		log.Println("err from creating new post request: ", err)
	}
	fmt.Println("about to exchange resource publication with oic/rd")
	message, err := conn.Exchange(msg)
	if err != nil {
		log.Println("err from publishing resources", err)
	}
	fmt.Println("response code for resource publication was: ", message.Code())
	promptUser(deviceID, accessToken, userID, conn)
}
