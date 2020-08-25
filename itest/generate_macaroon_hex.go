package main

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
)

func main() {
	variablePrefix := os.Args[1]
	macaroonFile := os.Args[2]
	certFile := os.Args[3]
	binary, _ := ioutil.ReadFile(macaroonFile)
	textCert, _ := ioutil.ReadFile(certFile)
	macHexa := hex.EncodeToString(binary)
	convertedCert := strings.ReplaceAll(string(textCert), "\n", "\\\\n")
	env := fmt.Sprintf("\n%v_CERT=\"%v\"\n%v_MACAROON_HEX=\"%v\"", variablePrefix, convertedCert, variablePrefix, macHexa)
	fmt.Println(env)
}
