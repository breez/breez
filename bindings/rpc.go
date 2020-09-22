package bindings

import (
	"context"
	"log"
	"net"
	"os"

	"github.com/breez/breez/data"
	"google.golang.org/grpc"
)

type RPC struct{}

func (r *RPC) GetLSPList(ctx context.Context, in *data.LSPListRequest) (
	*data.LSPList, error) {

	return getBreezApp().ServicesClient.LSPList()
}

func (r *RPC) ConnectToLSP(ctx context.Context, in *data.ConnectLSPRequest) (
	*data.ConnectLSPReply, error) {

	err := getBreezApp().AccountService.OpenLSPChannel(in.LspId)
	return &data.ConnectLSPReply{}, err
}

func (r *RPC) AddFundInit(ctx context.Context, in *data.AddFundInitRequest) (
	*data.AddFundInitReply, error) {
	return getBreezApp().SwapService.AddFundsInit(in.NotificationToken, in.LspID)
}

func (r *RPC) GetFundStatus(ctx context.Context, in *data.FundStatusRequest) (
	*data.FundStatusReply, error) {
	return getBreezApp().SwapService.GetFundStatus(in.NotificationToken)
}

func (r *RPC) AddInvoice(ctx context.Context, in *data.AddInvoiceRequest) (
	*data.AddInvoiceReply, error) {
	payreq, lspFee, err := getBreezApp().AccountService.AddInvoice(in)
	if err != nil {
		return nil, err
	}
	return &data.AddInvoiceReply{
		PaymentRequest: payreq,
		LspFee:         lspFee,
	}, nil
}

func (r *RPC) PayInvoice(ctx context.Context, in *data.PayInvoiceRequest) (
	*data.PaymentResponse, error) {
	var errorStr string
	traceReport, err := getBreezApp().AccountService.SendPaymentForRequest(
		in.PaymentRequest, in.Amount)
	if err != nil {
		errorStr = err.Error()
	}
	return &data.PaymentResponse{TraceReport: traceReport, PaymentError: errorStr}, nil
}

func (r *RPC) ListPayments(ctx context.Context, in *data.ListPaymentsRequest) (
	*data.PaymentsList, error) {
	return getBreezApp().AccountService.GetPayments()
}

func (r *RPC) RestartDaemon(ctx context.Context, in *data.RestartDaemonRequest) (
	*data.RestartDaemonReply, error) {
	if err := getBreezApp().RestartDaemon(); err != nil {
		return nil, err
	}
	return &data.RestartDaemonReply{}, nil
}

func (r *RPC) Start() {
	s := grpc.NewServer()
	data.RegisterBreezAPIServer(s, r)
	lisGRPC, err := net.Listen("tcp", os.Getenv("GRPC_LISTEN_ADDRESS"))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	if err := s.Serve(lisGRPC); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
