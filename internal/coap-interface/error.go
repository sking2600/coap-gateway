package main

import "fmt"

// Error errors type of coap-gateway
type Error string

func (e Error) Error() string { return string(e) }

// ErrEnvNotSet enviroment is not set
func ErrEnvNotSet(param string) error {
	return Error(fmt.Sprintf("Eenviroment variable '%v' is not set", param))
}

//ErrEmptyCARootPool ca root pool is empty
const ErrEmptyCARootPool = Error("CA Root pool is empty.")
