package satscard

import (
	"bytes"
	"fmt"
	"github.com/btcsuite/btcd/chaincfg"
	"math"

	"github.com/breez/breez/data"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

const (
	segwitDiscount   = 4.0
	segwitFlagVSize  = 0.5
	witnessItemVSize = (73.0 + 34.0) / segwitDiscount
)

func CreateSweepTransactions(slot *data.AddressInfo, recipient btcutil.Address, feeRates []int64, targetConfs []int32) ([]*data.RawSlotSweepTransaction, error) {
	if len(feeRates) != len(targetConfs) {
		return nil, fmt.Errorf("length mismatch between feeRates (%d) and targetConfs (%d)", len(feeRates), len(targetConfs))
	}

	feelessTx, err := createFeelessTransaction(slot, recipient)
	if err != nil {
		return nil, fmt.Errorf("failed to create feeless sweep transaction: %v", err)
	}
	vsize := calculateVSize(feelessTx)

	// Create copied transactions with the correct fee settings
	txs := make([]*data.RawSlotSweepTransaction, len(feeRates))
	for i := range txs {
		newTx := feelessTx.Copy()
		fee := int64(math.Ceil(float64(feeRates[i]) * vsize))
		newTx.TxOut[0].Value -= fee
		buffer := bytes.Buffer{}
		err := newTx.Serialize(&buffer)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize sweep transaction: %v", err)
		}
		txs[i] = &data.RawSlotSweepTransaction{
			MsgTx:               buffer.Bytes(),
			Input:               slot.ConfirmedBalance,
			Output:              newTx.TxOut[0].Value,
			VSize:               vsize,
			Fees:                fee,
			TargetConfirmations: targetConfs[i],
		}
	}
	return txs, nil
}

func SignTransaction(info *data.AddressInfo, transaction *data.RawSlotSweepTransaction, privateKeyBytes []byte, network *chaincfg.Params) (*data.TransactionDetails, error) {
	// Reconstruct the private key
	if len(privateKeyBytes) != btcec.PrivKeyBytesLen {
		return nil, fmt.Errorf("expected a private key of length %d but received %d bytes", btcec.PrivKeyBytesLen, len(privateKeyBytes))
	}
	privateKey, _ := btcec.PrivKeyFromBytes(privateKeyBytes)

	// Reconstruct the unsigned transaction
	tx := wire.NewMsgTx(wire.TxVersion)
	if err := tx.Deserialize(bytes.NewReader(transaction.MsgTx)); err != nil {
		return nil, fmt.Errorf("failed to deserialize unsigned sweep transaction: %v", err)
	}
	if len(tx.TxIn) != len(info.Utxos) {
		return nil, fmt.Errorf("length mismatch for the given UTXOs")
	}

	// All Satscard slots use a single P2WPKH address we can calculate their pay script locally
	slotAddress, err := btcutil.DecodeAddress(info.Address, network)
	if err != nil {
		return nil, fmt.Errorf("unable to decode Satscard slot address %s for network %s", info.Address, network.Name)
	}
	pkScript, err := txscript.PayToAddrScript(slotAddress)
	if err != nil {
		return nil, fmt.Errorf("unable to create public key script from address: %s", slotAddress.String())
	}

	// We need a way to map wire.OutPoint to a TxOut
	fetcherMap := make(map[wire.OutPoint]*wire.TxOut, len(tx.TxIn))
	for _, utxo := range info.Utxos {
		hash, err := chainhash.NewHashFromStr(utxo.Txid)
		if err != nil {
			return nil, err
		}
		outPoint := wire.NewOutPoint(hash, utxo.Vout)
		fetcherMap[*outPoint] = wire.NewTxOut(utxo.Value, pkScript)
	}
	fetcher := txscript.NewMultiPrevOutFetcher(fetcherMap)
	sigHashes := txscript.NewTxSigHashes(tx, fetcher)

	// Since Satscard slots all use P2WPKH addresses we can sign UTXOs the same way
	for i, txIn := range tx.TxIn {
		txOut := fetcherMap[txIn.PreviousOutPoint]
		witness, err := txscript.WitnessSignature(tx, sigHashes, i, txOut.Value, txOut.PkScript, txscript.SigHashAll, privateKey, true)
		if err != nil {
			return nil, fmt.Errorf("txscript.WitnessSignature: %v", err)
		}
		tx.TxIn[i].Witness = witness

		// Verify that the UTXO signature is valid
		flags := txscript.StandardVerifyFlags
		vm, err := txscript.NewEngine(txOut.PkScript, tx, i, flags, nil, sigHashes, txOut.Value, fetcher)
		if err != nil {
			return nil, fmt.Errorf("txscript.NewEngine: %v", err)
		}
		if err := vm.Execute(); err != nil {
			return nil, fmt.Errorf("txscript.Execute: %v", err)
		}
	}

	// Finally we can return our signed transaction
	buffer := bytes.Buffer{}
	err = tx.BtcEncode(&buffer, 0, wire.WitnessEncoding)
	if err != nil {
		return nil, fmt.Errorf("tx.BtcEncode: %v", err)
	}
	return &data.TransactionDetails{
		Tx:     buffer.Bytes(),
		TxHash: tx.TxHash().String(),
		Fees:   transaction.Fees,
	}, nil
}

func calculateVSize(tx *wire.MsgTx) float64 {
	// Satscards use single-sig P2WPKH inputs which makes the calculation simpler
	// Calculation found here: https://bitcoinops.org/en/tools/calc-size/
	numInputs := uint64(len(tx.TxIn))
	unsignedBytes := float64(tx.SerializeSizeStripped())
	witnessItemsLen := float64(wire.VarIntSerializeSize(1)) / segwitDiscount
	witnessItems := (witnessItemVSize + witnessItemsLen) * float64(numInputs)

	return unsignedBytes + segwitFlagVSize + witnessItems
}

func createFeelessTransaction(slot *data.AddressInfo, recipient btcutil.Address) (*wire.MsgTx, error) {
	outScript, err := txscript.PayToAddrScript(recipient)
	if err != nil {
		return nil, err
	}

	baseTx := wire.NewMsgTx(wire.TxVersion)
	baseTx.AddTxOut(wire.NewTxOut(slot.ConfirmedBalance, outScript))
	for _, utxo := range slot.Utxos {
		hash, err := chainhash.NewHashFromStr(utxo.Txid)
		if err != nil {
			return nil, err
		}
		prevOut := wire.NewOutPoint(hash, utxo.Vout)
		baseTx.AddTxIn(wire.NewTxIn(prevOut, nil, nil))
	}
	return baseTx, nil
}
