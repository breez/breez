module github.com/breez/breez

require (
	github.com/btcsuite/btcd v0.20.1-beta
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcutil v0.0.0-20190425235716-9e5f4b9a998d
	github.com/btcsuite/btcwallet v0.10.0
	github.com/btcsuite/btcwallet/walletdb v1.1.0
	github.com/btcsuite/btcwallet/wtxmgr v1.0.0
	github.com/coreos/bbolt v1.3.3
	github.com/golang/protobuf v1.3.1
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	github.com/lightninglabs/neutrino v0.10.0
	github.com/lightningnetwork/lnd v0.8.0-beta
	github.com/status-im/doubleratchet v0.0.0-20181102064121-4dcb6cba284a
	github.com/urfave/cli v1.18.0
	golang.org/x/net v0.0.0-20190503192946-f4e77d36d62c
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
	google.golang.org/api v0.13.0
	google.golang.org/grpc v1.20.1
	gopkg.in/macaroon.v2 v2.0.0
)

replace (
	git.schwanenlied.me/yawning/bsaes.git v0.0.0-20180720073208-c0276d75487e => github.com/Yawning/bsaes v0.0.0-20180720073208-c0276d75487e

	github.com/btcsuite/btcwallet v0.10.0 => github.com/breez/btcwallet v0.10.1-0.20191103104723-0e068b848776
	github.com/btcsuite/btcwallet/walletdb v1.1.0 => github.com/breez/btcwallet/walletdb v1.1.1-0.20191103104723-0e068b848776
	github.com/btcsuite/btcwallet/wtxmgr v1.0.0 => github.com/breez/btcwallet/wtxmgr v1.0.1-0.20191103104723-0e068b848776
	github.com/lightninglabs/neutrino => github.com/breez/neutrino v0.10.1-0.20191121084819-28462a8edb3a
	github.com/lightningnetwork/lnd => github.com/breez/lnd v0.8.0-beta-rc3.0.20191202085316-9ff5bc2a00f2
)

go 1.12
