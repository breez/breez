module github.com/breez/breez

require (
	github.com/FactomProject/basen v0.0.0-20150613233007-fe3947df716e // indirect
	github.com/FactomProject/btcutilecc v0.0.0-20130527213604-d3a63a5752ec // indirect
	github.com/breez/boltz v0.0.0-20210209180356-49e3bb7fe991
	github.com/breez/lspd v0.0.0-20210211151315-ece77f65e116
	github.com/btcsuite/btcd v0.23.1
	github.com/btcsuite/btcd/btcec/v2 v2.2.0
	github.com/btcsuite/btcd/btcutil v1.1.1 // indirect
	github.com/btcsuite/btcd/chaincfg/chainhash v1.0.1 // indirect
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcutil v1.0.2
	github.com/btcsuite/btcwallet v0.15.1
	github.com/btcsuite/btcwallet/walletdb v1.4.0
	github.com/btcsuite/btcwallet/wtxmgr v1.5.0
	github.com/cmars/basen v0.0.0-20150613233007-fe3947df716e // indirect
	github.com/decred/dcrd/lru v1.1.0 // indirect
	github.com/dustin/go-humanize v1.0.0
	github.com/fiatjaf/go-lnurl v1.10.1
	github.com/golang/protobuf v1.5.2
	github.com/jessevdk/go-flags v1.4.0
	github.com/kkdai/bstream v1.0.0 // indirect
	github.com/lightninglabs/neutrino v0.14.2
	github.com/lightninglabs/protobuf-hex-display v1.4.3-hex-display
	github.com/lightningnetwork/lnd v0.15.0-beta
	github.com/lightningnetwork/lnd/kvdb v1.3.1 // indirect
	github.com/status-im/doubleratchet v0.0.0-20181102064121-4dcb6cba284a
	github.com/tyler-smith/go-bip32 v0.0.0-20170922074101-2c9cfd177564
	github.com/urfave/cli v1.22.4
	go.etcd.io/bbolt v1.3.6
	golang.org/x/crypto v0.0.0-20210921155107-089bfa567519
	golang.org/x/net v0.0.0-20211015210444-4f30a5c0130f
	golang.org/x/oauth2 v0.0.0-20210615190721-d04028783cf1
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	google.golang.org/api v0.30.0
	google.golang.org/grpc v1.38.0
	google.golang.org/protobuf v1.26.0
	gopkg.in/macaroon.v2 v2.0.0
	launchpad.net/gocheck v0.0.0-20140225173054-000000000087 // indirect
)

replace (
	git.schwanenlied.me/yawning/bsaes.git => github.com/Yawning/bsaes v0.0.0-20180720073208-c0276d75487e
	github.com/btcsuite/btcwallet => github.com/breez/btcwallet v0.15.2-0.20220717090508-739787f948a6
	github.com/btcsuite/btcwallet/walletdb => github.com/breez/btcwallet/walletdb v1.4.1-0.20220717090508-739787f948a6
	github.com/btcsuite/btcwallet/wtxmgr => github.com/breez/btcwallet/wtxmgr v1.5.1-0.20220717090508-739787f948a6
	github.com/lightninglabs/neutrino => github.com/breez/neutrino v0.14.3-0.20220717090757-64cd9ef85ee7
	github.com/lightningnetwork/lnd => github.com/breez/lnd v0.15.0-beta.rc6.0.20220717110818-cf98538153c2
	github.com/lightningnetwork/lnd/cert => github.com/breez/lnd/cert v1.1.2-0.20220717110818-cf98538153c2
)

go 1.13
