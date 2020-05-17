module github.com/breez/breez

require (
	github.com/breez/boltz v0.0.0-20200125173807-d4eb28eda0f7
	github.com/btcsuite/btcd v0.20.1-beta
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcutil v0.0.0-20190425235716-9e5f4b9a998d
	github.com/btcsuite/btcwallet v0.11.1-0.20200219004649-ae9416ad7623
	github.com/btcsuite/btcwallet/walletdb v1.2.0
	github.com/btcsuite/btcwallet/wtxmgr v1.0.0
	github.com/coreos/bbolt v1.3.3
	github.com/fiatjaf/go-lnurl v0.0.0-20200322141859-984f796c1153
	github.com/golang/protobuf v1.3.3
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	github.com/lightninglabs/neutrino v0.11.0
	github.com/lightninglabs/protobuf-hex-display v1.3.3-0.20191212020323-b444784ce75d
	github.com/lightningnetwork/lnd v0.9.2-beta
	github.com/status-im/doubleratchet v0.0.0-20181102064121-4dcb6cba284a
	github.com/tidwall/gjson v1.6.0 // indirect
	github.com/urfave/cli v1.18.0
	golang.org/x/crypto v0.0.0-20200109152110-61a87790db17
	golang.org/x/mobile v0.0.0-20200329125638-4c31acba0007 // indirect
	golang.org/x/net v0.0.0-20191002035440-2ec189313ef0
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45
	golang.org/x/sync v0.0.0-20190911185100-cd5d95a43a6e
	google.golang.org/api v0.20.0
	google.golang.org/grpc v1.28.0
	gopkg.in/macaroon.v2 v2.0.0
)

replace (
	git.schwanenlied.me/yawning/bsaes.git => github.com/Yawning/bsaes v0.0.0-20180720073208-c0276d75487e
	github.com/btcsuite/btcwallet => github.com/breez/btcwallet v0.11.1-0.20200401131525-febcccb250bf
	github.com/btcsuite/btcwallet/walletdb v1.1.0 => github.com/breez/btcwallet/walletdb v1.2.1-0.20200401131525-febcccb250bf
	github.com/btcsuite/btcwallet/wtxmgr v1.0.0 => github.com/breez/btcwallet/wtxmgr v1.0.1-0.20200401131525-febcccb250bf
	github.com/lightninglabs/neutrino => github.com/breez/neutrino v0.11.1-0.20200329110104-3ef20f2cdeed
	github.com/lightningnetwork/lnd v0.9.2-beta => github.com/breez/lnd v0.9.2-beta.0.20200517155955-0f2183c92cad
	github.com/lightningnetwork/lnd/cert => github.com/lightningnetwork/lnd/cert v1.0.2-0.20200401010500-77df8e3a4386
)

go 1.12
