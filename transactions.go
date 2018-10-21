package breez

import (
	"context"
	"github.com/breez/lightninglib/lnrpc"
	"io"
	"time"
)

func watchSwapTransactions() {
	for {
		err := subscribeTransactionsOnce()
		if err != nil {
			log.Errorf("Error trying to watchSwapTransactions:", err)
		}
		time.Sleep(5 * time.Second)
	}
}

func subscribeTransactionsOnce() error {
	transactionStream, err := lightningClient.SubscribeTransactions(context.Background(), &lnrpc.GetTransactionsRequest{})
	if err != nil {
		return err
	}
	for {
		log.Infof("new Recv call")
		t, err := transactionStream.Recv()
		if err == io.EOF {
			log.Infof("Stream stopped. Need to re-registser")
			break
		}
		if err != nil {
			log.Infof("Error in stream: %v", err)
			return err
		}

		if t.NumConfirmations > 0 {
			fetchSwapAddresses(func(addr *swapAddressInfo) bool {
				for _, a := range t.DestAddresses {
					if a == addr.Address {
						sendSwapInvoice(t.DestAddresses[0], t.TxHash, t.Amount, addr.PaymentPreimage)
						return true
					}
				}
				return false
			})
		}

		// Save the swap transaction
		updateSwapAddressInfo(t.DestAddresses[0], func(addressInfo *swapAddressInfo) {
			if t.NumConfirmations > 0 {
				addressInfo.TransactionConfirmed = true
			}
			addressInfo.Transaction = t.TxHash
		})
	}
	return nil
}
