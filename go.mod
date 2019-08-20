module github.com/breez/breez

require (
	github.com/btcsuite/btcd v0.0.0-20190629003639-c26ffa870fd8
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcutil v0.0.0-20190425235716-9e5f4b9a998d
	github.com/btcsuite/btcwallet v0.0.0-20190814023431-505acf51507f
	github.com/coreos/bbolt v1.3.3
	github.com/golang/protobuf v1.3.1
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	github.com/lightninglabs/neutrino v0.0.0-20190629001446-52dd89dd1aaa
	github.com/lightningnetwork/lnd v0.7.0-beta
	github.com/status-im/doubleratchet v0.0.0-20181102064121-4dcb6cba284a
	github.com/urfave/cli v1.18.0
	golang.org/x/net v0.0.0-20190628185345-da137c7871d7
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
	google.golang.org/api v0.7.0
	google.golang.org/grpc v1.22.0
	gopkg.in/macaroon.v2 v2.0.0
)

replace (
	git.schwanenlied.me/yawning/bsaes.git v0.0.0-20180720073208-c0276d75487e => github.com/Yawning/bsaes v0.0.0-20180720073208-c0276d75487e
	github.com/btcsuite/btcwallet v0.0.0-20190814023431-505acf51507f => github.com/breez/btcwallet v0.0.0-20190820052426-2f8655345ca7
	github.com/lightninglabs/neutrino v0.0.0-20190629001446-52dd89dd1aaa => github.com/breez/neutrino v0.0.0-20190722075828-b444018978e0
	github.com/lightningnetwork/lnd v0.7.0-beta => github.com/breez/lnd v0.7.0-beta-rc2.0.20190715131809-0143c78887af

	golang.org/x/net => github.com/golang/net latest
)
