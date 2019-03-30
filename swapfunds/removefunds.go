package swapfunds

import (
	"context"

	breezservice "github.com/breez/breez/breez"
	"github.com/breez/breez/data"
	"github.com/breez/lightninglib/lnrpc"
)

/*
RemoveFund transfers the user funds from the chanel to a supplied on-chain address
It is executed in three steps:
1. Send the breez server an address and an amount and get a corresponding payment request
2. Pay the payment request.
3. Redeem the removed funds from the server
*/
func (s *Service) RemoveFund(amount int64, address string) (*data.RemoveFundReply, error) {
	c, ctx, cancel := s.breezServices.NewFundManager()
	defer cancel()
	reply, err := c.RemoveFund(ctx, &breezservice.RemoveFundRequest{Address: address, Amount: amount})
	if err != nil {
		s.log.Errorf("RemoveFund: server endpoint call failed: %v", err)
		return nil, err
	}
	if reply.ErrorMessage != "" {
		return &data.RemoveFundReply{ErrorMessage: reply.ErrorMessage}, nil
	}

	s.log.Infof("RemoveFunds: got payment request: %v", reply.PaymentRequest)
	payreq, err := s.lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: reply.PaymentRequest})
	if err != nil {
		s.log.Errorf("DecodePayReq of server response failed: %v", err)
		return nil, err
	}

	//mark this payment request as redeemable
	s.breezDB.AddRedeemablePaymentHash(payreq.PaymentHash)

	s.log.Infof("RemoveFunds: Sending payment...")
	err = s.sendPayment(reply.PaymentRequest, 0)
	if err != nil {
		s.log.Errorf("SendPaymentForRequest failed: %v", err)
		return nil, err
	}
	s.log.Infof("SendPaymentForRequest finished successfully")
	txID, err := s.redeemRemovedFundsForHash(payreq.PaymentHash)
	if err != nil {
		s.log.Errorf("RedeemRemovedFunds failed: %v", err)
		return nil, err
	}
	s.log.Infof("RemoveFunds finished successfully")
	return &data.RemoveFundReply{ErrorMessage: "", Txid: txID}, err
}

func (s *Service) redeemAllRemovedFunds() error {
	s.log.Infof("redeemAllRemovedFunds")
	hashes, err := s.breezDB.FetchRedeemablePaymentHashes()
	if err != nil {
		s.log.Errorf("failed to fetchRedeemablePaymentHashes, %v", err)
		return err
	}
	for _, hash := range hashes {
		s.log.Infof("Redeeming transaction for has %v", hash)
		txID, err := s.redeemRemovedFundsForHash(hash)
		if err != nil {
			s.log.Errorf("failed to redeem funds for hash %v, %v", hash, err)
		} else {
			s.log.Infof("successfully redeemed funds for hash %v, txid=%v", hash, txID)
		}
	}
	return err
}

func (s *Service) redeemRemovedFundsForHash(hash string) (string, error) {
	fundManager, ctx, cancel := s.breezServices.NewFundManager()
	defer cancel()
	redeemReply, err := fundManager.RedeemRemovedFunds(ctx, &breezservice.RedeemRemovedFundsRequest{Paymenthash: hash})
	if err != nil {
		s.log.Errorf("RedeemRemovedFunds failed for hash: %v,   %v", hash, err)
		return "", err
	}
	return redeemReply.Txid, s.breezDB.UpdateRedeemTxForPayment(hash, redeemReply.Txid)
}
