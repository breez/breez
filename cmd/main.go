package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/breez/breez/bindings"
	"github.com/breez/breez/data"
	cli "github.com/deadsy/go-cli"
	"github.com/golang/protobuf/proto"
)

var cmdHelp = cli.Leaf{
	Descr: "general help",
	F: func(c *cli.CLI, args []string) {
		c.GeneralHelp()
	},
}

var cmdHistory = cli.Leaf{
	Descr: "command history",
	F: func(c *cli.CLI, args []string) {
		c.SetLine(c.DisplayHistory(args))
	},
}

var cmdExit = cli.Leaf{
	Descr: "exit application",
	F: func(c *cli.CLI, args []string) {
		c.Exit()
	},
}

var cmdNewReverseSwap = cli.Leaf{
	Descr: "create a new reverse swap",
	F: func(c *cli.CLI, args []string) {
		if len(args) < 2 {
			fmt.Println("Error: Need an amount and an address!")
			return
		}
		amt, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Println(fmt.Errorf("strconv.Atoi(%v): %w", args[0], err))
			return
		}
		req, err := proto.Marshal(&data.ReverseSwapRequest{Amount: int64(amt), Address: args[1]})
		if err != nil {
			fmt.Println(fmt.Errorf("proto.Marshal(%#v): %w", data.ReverseSwapRequest{Amount: int64(amt), Address: args[1]}, err))
			return
		}
		h, err := bindings.NewReverseSwap(req)
		if err != nil {
			fmt.Println(fmt.Errorf("bindings.NewReverseSwap(%#v): %w", data.ReverseSwapRequest{Amount: int64(amt), Address: args[1]}, err))
			return
		}
		fmt.Printf("%s\n", h)
	},
}
var cmdFetchReverseSwap = cli.Leaf{
	Descr: "fetch reverse swap info",
	F: func(c *cli.CLI, args []string) {
		if len(args) < 1 {
			fmt.Println("need the hash of a payment preimage!")
			return
		}
		b, err := bindings.FetchReverseSwap(args[0])
		if err != nil {
			fmt.Println(fmt.Errorf("bindings.FetchReverseSwap(%v): %w", args[0], err))
			return
		}
		var rs data.ReverseSwap
		err = proto.Unmarshal(b, &rs)
		if err != nil {
			fmt.Println(fmt.Errorf("proto.Unmarshal(%x): %w", b, err))
			return
		}
		fmt.Printf("%#v\n", rs)
	},
}
var cmdReverseSwapClaimFeeEstimates = cli.Leaf{
	Descr: "get blocks/fee pairs for the claim transaction of a reverse swap",
	F: func(c *cli.CLI, args []string) {
		if len(args) < 1 {
			fmt.Println("need a claim address!")
			return
		}
		b, err := bindings.ReverseSwapClaimFeeEstimates(args[0])
		if err != nil {
			fmt.Println(fmt.Errorf("bindings.ReverseSwapClaimFeeEstimates(%v): %w", args[0], err))
			return
		}
		var fees data.ClaimFeeEstimates
		err = proto.Unmarshal(b, &fees)
		if err != nil {
			fmt.Println(fmt.Errorf("proto.Unmarshal(%x): %w", b, err))
			return
		}
		fmt.Printf("%#v\n", fees.Fees)
	},
}
var cmdSetReverseSwapClaimFee = cli.Leaf{
	Descr: "set the fee for the claim transaction of a reverse swap",
	F: func(c *cli.CLI, args []string) {
		if len(args) < 2 {
			fmt.Println("Error: Need a hash and a fee!")
			return
		}
		fee, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Println(fmt.Errorf("strconv.Atoi(%v): %w", args[1], err))
			return
		}
		req, err := proto.Marshal(&data.ReverseSwapClaimFee{Hash: args[0], Fee: int64(fee)})
		if err != nil {
			fmt.Println(fmt.Errorf("proto.Marshal(%#v): %w", data.ReverseSwapClaimFee{Hash: args[0], Fee: int64(fee)}, err))
			return
		}
		err = bindings.SetReverseSwapClaimFee(req)
		if err != nil {
			fmt.Println(fmt.Errorf("bindings.SetReverseSwapClaimFee(%#v): %w", data.ReverseSwapClaimFee{Hash: args[0], Fee: int64(fee)}, err))
			return
		}
		fmt.Printf("done.\n")
	},
}
var cmdPayReverseSwap = cli.Leaf{
	Descr: "pay reverse swap ln invoice",
	F: func(c *cli.CLI, args []string) {
		if len(args) < 1 {
			fmt.Println("need the hash of a payment preimage!")
			return
		}
		req, err := proto.Marshal(&data.ReverseSwapPaymentRequest{Hash: args[0], DeviceId: "<NA>"})
		if err != nil {
			fmt.Println(fmt.Errorf("proto.Marshal(%#v): %w", data.ReverseSwapPaymentRequest{Hash: args[0], DeviceId: "<NA>"}, err))
			return
		}
		err = bindings.PayReverseSwap(req)
		if err != nil {
			fmt.Println(fmt.Errorf("bindings.PayReverseSwap(%v): %w", args[0], err))
			return
		}
	},
}
var cmdReverseSwapPaymentStatuses = cli.Leaf{
	Descr: "get the status of the in-flight reverse swap payments",
	F: func(c *cli.CLI, args []string) {
		b, err := bindings.ReverseSwapPayments()
		if err != nil {
			fmt.Println(fmt.Errorf("bindings.ReverseSwapPayments(): %w", err))
			return
		}
		var s data.ReverseSwapPaymentStatuses
		err = proto.Unmarshal(b, &s)
		if err != nil {
			fmt.Println(fmt.Errorf("proto.Unmarshal(%x): %w", b, err))
			return
		}
		for _, ps := range s.PaymentsStatus {
			fmt.Printf("hash:%v,eta:%v\n", ps.Hash, ps.Eta)
		}
	},
}

var cmdUnconfirmedReverseSwapClaimTransaction = cli.Leaf{
	Descr: "get the txid of the unconfirmed reverse swap claim transaction, if any",
	F: func(c *cli.CLI, args []string) {
		h, err := bindings.UnconfirmedReverseSwapClaimTransaction()
		fmt.Printf("txid: %v, err: %v\n", h, err)
	},
}

var menuRoot = cli.Menu{
	{"exit", cmdExit},
	{"help", cmdHelp},
	{"history", cmdHistory, cli.HistoryHelp},

	{"newreverseswap", cmdNewReverseSwap},
	{"fetchreverseswap", cmdFetchReverseSwap},
	{"claimfeeestimates", cmdReverseSwapClaimFeeEstimates},
	{"setreverseswapclaimfee", cmdSetReverseSwapClaimFee},
	{"payreverseswap", cmdPayReverseSwap},
	{"reverseswapstatus", cmdReverseSwapPaymentStatuses},
	{"unconfirmedreverseswapclaimtransaction", cmdUnconfirmedReverseSwapClaimTransaction},
}

type breezApp struct{}

func newBreezApp() *breezApp {
	return &breezApp{}
}

func (app *breezApp) Put(s string) {
	fmt.Printf("%s", s)
}

type ServicesImpl struct {
}

func (a *ServicesImpl) Notify(notificationEvent []byte) {}

func (a *ServicesImpl) BackupProviderSignIn() (string, error) {
	return "", nil
}

func (a *ServicesImpl) BackupProviderName() string {
	return "gdrive"
}

func main() {
	workingDir := os.Getenv("LND_DIR")

	err := bindings.Init(os.TempDir(), workingDir, &ServicesImpl{})
	if err != nil {
		fmt.Println("Error in binding.Init", err)
		os.Exit(1)
	}

	err = bindings.Start()
	if err != nil {
		fmt.Println("Error in binding.Start", err)
		os.Exit(1)
	}

	//hPath := filepath.Join(workingDir, "breez_history.txt")
	hPath := "history.txt"
	app := newBreezApp()
	c := cli.NewCLI(app)
	//c.SetPrompt("")
	c.HistoryLoad(hPath)

	c.SetRoot(menuRoot)
	for c.Running() {
		c.Run()
	}
	c.HistorySave(hPath)

}
