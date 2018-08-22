package main

import (
	"log"
)

//constants
var (
	envKeepaliveTime     = "KEEPALIVE_TIME"
	envKeepaliveInterval = "KEEPALIVE_INTERVAL"
	envKeepaliveRetry    = "KEEPALIVE_RETRY"
	envListenAddress     = "ADDRESS"
	envListenNet         = "NETWORK"
)

func main() {

	//run server
	log.Fatal(NewServer().ListenAndServe())
}
