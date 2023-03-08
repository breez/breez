package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/btcsuite/btcd/btcec/v2"
)

type NodeConfig struct {
	Name                      string     `json:name,omitempty`
	NodePubkey                string     `json:nodePubkey,omitempty`
	LspdPrivateKey            string     `json:"lspdPrivateKey"`
	Token                     string     `json:"token"`
	Host                      string     `json:"host"`
	PublicChannelAmount       int64      `json:"publicChannelAmount,string"`
	ChannelAmount             uint64     `json:"channelAmount,string"`
	ChannelPrivate            bool       `json:"channelPrivate"`
	TargetConf                uint32     `json:"targetConf,string"`
	MinHtlcMsat               uint64     `json:"minHtlcMsat,string"`
	BaseFeeMsat               uint64     `json:"baseFeeMsat,string"`
	FeeRate                   float64    `json:"feeRate,string"`
	TimeLockDelta             uint32     `json:"timeLockDelta,string"`
	ChannelFeePermyriad       int64      `json:"channelFeePermyriad,string"`
	ChannelMinimumFeeMsat     int64      `json:"channelMinimumFeeMsat,string"`
	AdditionalChannelCapacity int64      `json:"additionalChannelCapacity,string"`
	MaxInactiveDuration       uint64     `json:"maxInactiveDuration,string"`
	Lnd                       *LndConfig `json:"lnd,omitempty"`
	Cln                       *ClnConfig `json:"cln,omitempty"`
}

type LndConfig struct {
	Address  string `json:"address"`
	Cert     string `json:"cert"`
	Macaroon string `json:"macaroon"`
}

type ClnConfig struct {
	PluginAddress string `json:"pluginAddress"`
	SocketPath    string `json:"socketPath"`
}

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

	config := NodeConfig{
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
		Token:                     "8qFbOxF8K8frgrhNE/Hq/UkUlq7A1Qvh8um1VdCUv2L4es/RXEe500E+FAKkLI4X",
	}
	config.Lnd = &LndConfig{
		Address:  address,
		Cert:     string(textCert),
		Macaroon: macHexa,
	}
	json, _ := json.Marshal([]NodeConfig{config})
	fmt.Printf(fmt.Sprintf("\nNODES='%v'\n", string(json)))
}
