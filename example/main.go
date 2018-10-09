package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/breez/breez"
	"github.com/breez/breez/data"
)

func main() {

	notifChannel, err := breez.Start(os.Getenv("LND_DIR"))
	if err != nil {
		fmt.Println("Error starting breez", err)
		os.Exit(1)
	}

	go printNotifications(notifChannel)
	time.Sleep(60000 * time.Second)
}

func printNotifications(notifChan chan data.NotificationEvent) {
	for {
		event := <-notifChan
		log.Println("Got event", event)
		switch event.Type {
		case data.NotificationEvent_ACCOUNT_CHANGED:
			acc, err := breez.GetAccountInfo()
			if err != nil {
				breez.Log(err.Error(), "SEVERE")

			}
			breez.Log("Account status= "+acc.Status.String(), "INFO")
			breez.Log("Account balance= "+strconv.FormatInt(acc.Balance, 10), "INFO")
			breez.Log("Account remote balance = "+strconv.FormatInt(acc.RemoteBalance, 10), "INFO")
			breez.Log("Account block = "+strconv.Itoa(int(acc.ChainStatus.BlockHeight)), "INFO")
			paymentsList, err := breez.GetPayments()
			if err != nil {
				breez.Log(err.Error(), "SEVERE")
			}
			breez.Log("Payments = "+paymentsList.String(), "INFO")
		case data.NotificationEvent_INVOICE_PAID:
			paymentsList, err := breez.GetPayments()
			if err != nil {
				breez.Log(err.Error(), "SEVERE")
			}
			breez.Log("Invoice paid! Payments = "+paymentsList.String(), "INFO")
		case data.NotificationEvent_READY:
			payReq, err := breez.AddInvoice(&data.InvoiceMemo{Amount: 100000, Description: "Test Invoice", PayeeImageURL: "https://breez.technology"})
			if err != nil {
				breez.Log(err.Error(), "SEVERE")
			}
			invoice, err := breez.DecodePaymentRequest(payReq)
			if err != nil {
				breez.Log(err.Error(), "SEVERE")
			}
			breez.Log("Invoice Details", "INFO")
			breez.Log("Amounts "+strconv.Itoa(int(invoice.Amount)), "INFO")
			breez.Log("Description "+invoice.Description, "INFO")
			breez.Log("Image URL "+invoice.PayeeImageURL, "INFO")
		}
	}
}
