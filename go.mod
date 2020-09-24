module github.com/breez/breez

require (
	github.com/breez/boltz v0.0.0-20200125173807-d4eb28eda0f7
	github.com/btcsuite/btcd v0.20.1-beta.0.20200730232343-1db1b6f8217f
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcutil v1.0.2
	github.com/btcsuite/btcwallet v0.11.1-0.20200814001439-1d31f4ea6fc5
	github.com/btcsuite/btcwallet/walletdb v1.3.4-0.20200616004619-ca24ed58cf8a
	github.com/btcsuite/btcwallet/wtxmgr v1.2.1-0.20200616004619-ca24ed58cf8a
	github.com/coreos/bbolt v1.3.3
	github.com/dustin/go-humanize v1.0.0
	github.com/fiatjaf/go-lnurl v0.0.0-20200322141859-984f796c1153
	github.com/golang/protobuf v1.4.2
	github.com/jessevdk/go-flags v1.4.0
	github.com/lightninglabs/neutrino v0.11.1-0.20200430233911-38bf97e348a3
	github.com/lightninglabs/protobuf-hex-display v1.3.3-0.20191212020323-b444784ce75d
	github.com/lightningnetwork/lnd v0.10.0-beta.rc6.0.20200708211108-8cb1276dbf0b
	github.com/status-im/doubleratchet v0.0.0-20181102064121-4dcb6cba284a
	github.com/tidwall/gjson v1.6.0 // indirect
	github.com/urfave/cli v1.18.0
	golang.org/x/crypto v0.0.0-20200709230013-948cd5f35899
	golang.org/x/mobile v0.0.0-20200801112145-973feb4309de // indirect
	golang.org/x/net v0.0.0-20191002035440-2ec189313ef0
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45
	golang.org/x/sync v0.0.0-20190911185100-cd5d95a43a6e
	golang.org/x/sys v0.0.0-20191008105621-543471e840be // indirect
	golang.org/x/tools v0.0.0-20200117012304-6edc0a871e69 // indirect
	google.golang.org/api v0.20.0
	google.golang.org/grpc v1.28.0
	google.golang.org/protobuf v1.25.0
	gopkg.in/macaroon.v2 v2.0.0
)

replace (
	git.schwanenlied.me/yawning/bsaes.git => github.com/Yawning/bsaes v0.0.0-20180720073208-c0276d75487e
	github.com/btcsuite/btcwallet => github.com/breez/btcwallet v0.11.1-0.20200726120254-83f8f93c32e9
	github.com/btcsuite/btcwallet/walletdb => github.com/breez/btcwallet/walletdb v1.3.2-0.20200726120254-83f8f93c32e9
	github.com/btcsuite/btcwallet/wtxmgr => github.com/breez/btcwallet/wtxmgr v1.1.1-0.20200726120254-83f8f93c32e9
	github.com/lightninglabs/neutrino => github.com/breez/neutrino v0.11.1-0.20200707124028-efe899f4cedb
	github.com/lightningnetwork/lnd => github.com/breez/lnd v0.11.0-beta.rc4.0.20200924114617-81301ddd166e
	github.com/lightningnetwork/lnd/cert => github.com/lightningnetwork/lnd/cert v1.0.2-0.20200401010500-77df8e3a4386
)

go 1.13
