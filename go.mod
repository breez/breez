module github.com/breez/breez

require (
	github.com/awalterschulze/gographviz v0.0.0-20160912181450-761fd5fbb34e
	github.com/breez/lightninglib v0.0.0-20181105101218-250c92936df2
	github.com/btcsuite/btcd v0.0.0-20180903232927-cff30e1d23fc
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcutil v0.0.0-20180706230648-ab6388e0c60a
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b // indirect
	github.com/golang/protobuf v1.2.0
	github.com/jessevdk/go-flags v1.4.0
	github.com/urfave/cli v1.18.0
	go.etcd.io/bbolt v1.3.0
	golang.org/x/mobile v0.0.0-20180922163855-920b52be609a // indirect
	golang.org/x/net v0.0.0-20180911220305-26e67e76b6c3
	golang.org/x/sync v0.0.0-20180314180146-1d60e4601c6f
	google.golang.org/grpc v1.14.0
	gopkg.in/macaroon.v2 v2.0.0
)

replace github.com/btcsuite/btcd v0.0.0-20180903232927-cff30e1d23fc => github.com/breez/btcd v0.0.0-20181025150601-acccfea9669b

replace github.com/lightninglabs/neutrino v0.0.0-20181019013733-8018ab76e70a => github.com/breez/neutrino v0.0.0-20181025065603-14bd66aff1db

replace github.com/btcsuite/btcwallet v0.0.0-20181017015332-c4dd27e481f9 => github.com/breez/btcwallet v0.0.0-20181025062152-a1da5037baf7
