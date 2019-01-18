package main

import (
	"fmt"
	"net/http"
	"strings"
)

func main() {
	_, err := http.Post("35.232.51.254:8080/test-uuid/test-href", "application/json", strings.NewReader("test-post"))
	fmt.Println(err)
}
