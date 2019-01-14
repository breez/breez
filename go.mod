module github.com/breez/breez

require (
	github.com/awalterschulze/gographviz v0.0.0-20160912181450-761fd5fbb34e
	github.com/breez/lightninglib v0.0.0-20190104154320-c0fe578ef0b0
	github.com/btcsuite/btcd v0.0.0-20180903232927-cff30e1d23fc
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcutil v0.0.0-20180706230648-ab6388e0c60a
	github.com/btcsuite/btcwallet v0.0.0-20181120233725-7ad4f1e81d78
	github.com/golang/protobuf v1.2.0
	github.com/grpc-ecosystem/go-grpc-middleware v1.0.0 // indirect
	github.com/jessevdk/go-flags v1.4.0
	github.com/lightninglabs/neutrino v0.0.0-20181102193151-641af1ca5561
	github.com/status-im/doubleratchet v0.0.0-20181102064121-4dcb6cba284a
	github.com/urfave/cli v1.18.0
	go.etcd.io/bbolt v1.3.0
	golang.org/x/mobile v0.0.0-20181026062114-a27dd33d354d // indirect
	golang.org/x/net v0.0.0-20180911220305-26e67e76b6c3
	golang.org/x/sync v0.0.0-20180314180146-1d60e4601c6f
	google.golang.org/grpc v1.16.0
	gopkg.in/macaroon.v2 v2.0.0
)

replace github.com/btcsuite/btcd v0.0.0-20180903232927-cff30e1d23fc => github.com/breez/btcd v0.0.0-20181025150601-acccfea9669b

replace github.com/btcsuite/btcwallet v0.0.0-20181120233725-7ad4f1e81d78 => github.com/breez/btcwallet v0.0.0-20181125122430-d942b679e4d4

replace github.com/lightninglabs/neutrino v0.0.0-20181102193151-641af1ca5561 => github.com/breez/neutrino v0.0.0-20190114145151-3b06e2d8a6dd
