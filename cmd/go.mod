module github.com/breez/breez/cmd

go 1.13

require (
	github.com/breez/breez v0.0.0-20191222101539-d9f37e24beee
	github.com/deadsy/go-cli v0.0.0-20191117003156-1fbe7fd20d78
	github.com/golang/protobuf v1.3.1
	github.com/lightningnetwork/lnd v0.8.0-beta-rc3.0.20191221022352-72a49d486ae4
)

replace (
	github.com/breez/breez => ../
	github.com/btcsuite/btcd v0.20.0-beta => github.com/btcsuite/btcd v0.20.1-beta
	github.com/btcsuite/btcwallet v0.10.0 => github.com/breez/btcwallet v0.10.1-0.20191121081139-3f579e0a038c
	github.com/btcsuite/btcwallet/walletdb v1.1.0 => github.com/breez/btcwallet/walletdb v1.1.1-0.20191121081139-3f579e0a038c
	github.com/btcsuite/btcwallet/wtxmgr v1.0.0 => github.com/breez/btcwallet/wtxmgr v1.0.1-0.20191121081139-3f579e0a038c
	github.com/lightninglabs/neutrino => github.com/breez/neutrino v0.0.0-20200121095351-60bf3b6b810e
	github.com/lightningnetwork/lnd => github.com/breez/lnd v0.8.0-beta-rc3.0.20200116133631-c9a52c9af49a
)
