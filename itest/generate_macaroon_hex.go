package main

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
)

func main() {
	macaroonFile := os.Args[1]
	certFile := os.Args[2]
	binary, _ := ioutil.ReadFile(macaroonFile)
	textCert, _ := ioutil.ReadFile(certFile)
	macHexa := hex.EncodeToString(binary)
	convertedCert := strings.ReplaceAll(string(textCert), "\n", "\\\\n")
	env := fmt.Sprintf("\nLND_CERT=\"%v\"\nLND_MACAROON_HEX=\"%v\"", convertedCert, macHexa)
	fmt.Println(env)
}
