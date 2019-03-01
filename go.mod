module github.com/breez/breez

require (
	github.com/awalterschulze/gographviz v0.0.0-20160912181450-761fd5fbb34e
	github.com/breez/lightninglib v0.0.0-20190228155423-b7fe8efb5798
	github.com/btcsuite/btcd v0.0.0-20190115013929-ed77733ec07d
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcutil v0.0.0-20190112041146-bf1e1be93589
	github.com/btcsuite/btcwallet v0.0.0-20190123033236-ba03278a64bc
	github.com/golang/protobuf v1.2.0
	github.com/grpc-ecosystem/go-grpc-middleware v1.0.0 // indirect
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	github.com/lightninglabs/neutrino v0.0.0-20190115022559-351f5f06c6af
	github.com/status-im/doubleratchet v0.0.0-20181102064121-4dcb6cba284a
	github.com/urfave/cli v1.18.0
	go.etcd.io/bbolt v1.3.0
	golang.org/x/mobile v0.0.0-20181026062114-a27dd33d354d // indirect
	golang.org/x/net v0.0.0-20181106065722-10aee1819953
	golang.org/x/oauth2 v0.0.0-20181203162652-d668ce993890
	golang.org/x/sync v0.0.0-20181108010431-42b317875d0f
	google.golang.org/api v0.1.0
	google.golang.org/grpc v1.18.0
	gopkg.in/macaroon.v2 v2.0.0
)

replace github.com/btcsuite/btcd v0.0.0-20190115013929-ed77733ec07d => github.com/breez/btcd v0.0.0-20190217135408-786ef411fa65

replace github.com/lightninglabs/neutrino v0.0.0-20190115022559-351f5f06c6af => github.com/breez/neutrino v0.0.0-20190217133150-08c3c9e7d6d5

replace github.com/btcsuite/btcwallet v0.0.0-20190123033236-ba03278a64bc => github.com/breez/btcwallet v0.0.0-20190217161832-505bba602ae1
