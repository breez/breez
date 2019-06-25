module github.com/breez/breez

require (
	github.com/awalterschulze/gographviz v0.0.0-20190522210029-fa59802746ab // indirect
	github.com/btcsuite/btcd v0.0.0-20190614013741-962a206e94e9
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcutil v0.0.0-20190425235716-9e5f4b9a998d
	github.com/btcsuite/btcwallet v0.0.0-20190620233257-46c0cf2a3f3a
	github.com/golang/protobuf v1.3.1
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	github.com/lightninglabs/neutrino v0.0.0-20190620074315-32af2fbd0d2b
	github.com/lightningnetwork/lnd v0.7.0-beta-rc2
	github.com/status-im/doubleratchet v0.0.0-20181102064121-4dcb6cba284a
	github.com/urfave/cli v1.18.0
	go.etcd.io/bbolt v1.3.2
	golang.org/x/net v0.0.0-20190620200207-3b0461eec859
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
	google.golang.org/api v0.6.0
	google.golang.org/grpc v1.21.1
	gopkg.in/macaroon.v2 v2.0.0
)

replace github.com/lightningnetwork/lnd v0.7.0-beta-rc2 => github.com/breez/lnd v0.6.1-beta.0.20190625115649-d3889f041b14

replace github.com/btcsuite/btcwallet v0.0.0-20190620233257-46c0cf2a3f3a => github.com/breez/btcwallet v0.0.0-20190623095413-2d15a1bc5050