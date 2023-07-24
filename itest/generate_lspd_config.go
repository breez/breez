package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/breez/lspd/config"
	"github.com/btcsuite/btcd/btcec/v2"
)

func main() {
	address := os.Args[1]
	macaroonFile := os.Args[2]
	certFile := os.Args[3]
	nodePubKey := os.Args[4]
	nodeHost := os.Args[5]
	binary, _ := ioutil.ReadFile(macaroonFile)
	textCert, _ := ioutil.ReadFile(certFile)
	macHexa := hex.EncodeToString(binary)
	//convertedCert := strings.ReplaceAll(string(textCert), "\n", "\\\\n")
	//env := fmt.Sprintf("\n%v_CERT=\"%v\"\n%v_MACAROON_HEX=\"%v\"", variablePrefix, convertedCert, variablePrefix, macHexa)
	//fmt.Println(env)

	p, err := btcec.NewPrivateKey()
	if err != nil {
		log.Fatalf("btcec.NewPrivateKey() error: %v", err)
	}
	key := hex.EncodeToString(p.Serialize())

	cfg := config.NodeConfig{
		LspdPrivateKey:            key,
		Host:                      nodeHost,
		NodePubkey:                nodePubKey,
		AdditionalChannelCapacity: 100000,
		BaseFeeMsat:               1000,
		FeeRate:                   0.000001,
		ChannelMinimumFeeMsat:     2000000,
		ChannelFeePermyriad:       40,
		MinHtlcMsat:               600,
		TimeLockDelta:             144,
		Tokens:                    []string{"8qFbOxF8K8frgrhNE/Hq/UkUlq7A1Qvh8um1VdCUv2L4es/RXEe500E+FAKkLI4X"},
	}
	cfg.Lnd = &config.LndConfig{
		Address:  address,
		Cert:     string(textCert),
		Macaroon: macHexa,
	}
	json, _ := json.Marshal([]config.NodeConfig{cfg})
	fmt.Printf(fmt.Sprintf("\nNODES='%v'\n", string(json)))
}
