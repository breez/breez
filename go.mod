module github.com/breez/breez

require (
	github.com/FactomProject/basen v0.0.0-20150613233007-fe3947df716e // indirect
	github.com/FactomProject/btcutilecc v0.0.0-20130527213604-d3a63a5752ec // indirect
	github.com/breez/boltz v0.0.0-20210209180356-49e3bb7fe991
	github.com/breez/lspd v0.0.0-20210211151315-ece77f65e116
	github.com/btcsuite/btcd v0.21.0-beta.0.20201208033208-6bd4c64a54fa
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcutil v1.0.2
	github.com/btcsuite/btcwallet v0.11.1-0.20201207233335-415f37ff11a1
	github.com/btcsuite/btcwallet/walletdb v1.3.4
	github.com/btcsuite/btcwallet/wtxmgr v1.2.1-0.20200616004619-ca24ed58cf8a
	github.com/cmars/basen v0.0.0-20150613233007-fe3947df716e // indirect
	go.etcd.io/bbolt v1.3.5-0.20200615073812-232d8fc87f50
	github.com/dustin/go-humanize v1.0.0
	github.com/fiatjaf/go-lnurl v1.1.1
	github.com/golang/protobuf v1.4.2
	github.com/jessevdk/go-flags v1.4.0
	github.com/lightninglabs/neutrino v0.11.1-0.20201210023533-e1978372d15e
	github.com/lightninglabs/protobuf-hex-display v1.3.3-0.20191212020323-b444784ce75d
	github.com/lightningnetwork/lnd v0.11.0-beta
	github.com/status-im/doubleratchet v0.0.0-20181102064121-4dcb6cba284a
	github.com/tyler-smith/go-bip32 v0.0.0-20170922074101-2c9cfd177564
	github.com/urfave/cli v1.22.1
	golang.org/x/crypto v0.0.0-20200709230013-948cd5f35899
	golang.org/x/mobile v0.0.0-20210220033013-bdb1ca9a1e08 // indirect
	golang.org/x/net v0.0.0-20200301022130-244492dfa37a
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d
	golang.org/x/sync v0.0.0-20190911185100-cd5d95a43a6e
	google.golang.org/api v0.20.0
	google.golang.org/genproto v0.0.0-20200806141610-86f49bd18e98 // indirect
	google.golang.org/grpc v1.29.1
	google.golang.org/protobuf v1.25.0
	gopkg.in/macaroon.v2 v2.0.0
	launchpad.net/gocheck v0.0.0-20140225173054-000000000087 // indirect
)

replace (
	git.schwanenlied.me/yawning/bsaes.git => github.com/Yawning/bsaes v0.0.0-20180720073208-c0276d75487e
	github.com/btcsuite/btcwallet => github.com/breez/btcwallet v0.11.1-0.20210414123232-efee8e15b2ad
	github.com/btcsuite/btcwallet/walletdb => github.com/breez/btcwallet/walletdb v1.3.5-0.20210414123232-efee8e15b2ad
	github.com/btcsuite/btcwallet/wtxmgr => github.com/breez/btcwallet/wtxmgr v1.2.1-0.20210414123232-efee8e15b2ad
	github.com/lightninglabs/neutrino => github.com/breez/neutrino v0.11.1-0.20201009140859-946fb9f51bc0
	github.com/lightningnetwork/lnd => github.com/breez/lnd v0.12.1-beta.rc6.0.20210414123723-e06a30d26707
	github.com/lightningnetwork/lnd/cert => github.com/lightningnetwork/lnd/cert v1.0.2-0.20200401010500-77df8e3a4386
)

go 1.13
