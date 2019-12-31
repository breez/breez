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
		h, err := bindings.NewReverseSwap(int64(amt), args[1])
		if err != nil {
			fmt.Println(fmt.Errorf("bindings.NewReverseSwap(%v): %w", amt, err))
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
var cmdPayReverseSwap = cli.Leaf{
	Descr: "pay reverse swap ln invoice",
	F: func(c *cli.CLI, args []string) {
		if len(args) < 1 {
			fmt.Println("need the hash of a payment preimage!")
			return
		}
		err := bindings.PayReverseSwap(args[0])
		if err != nil {
			fmt.Println(fmt.Errorf("bindings.PayReverseSwap(%v): %w", args[0], err))
			return
		}
	},
}

var menuRoot = cli.Menu{
	{"exit", cmdExit},
	{"help", cmdHelp},
	{"history", cmdHistory, cli.HistoryHelp},

	{"newreverseswap", cmdNewReverseSwap},
	{"fetchreverseswap", cmdFetchReverseSwap},
	{"payreverseswap", cmdPayReverseSwap},
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
