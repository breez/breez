FROM golang:1.16-alpine3.12 AS builder
RUN apk update
RUN apk add git go musl-dev make bash
RUN export tags="experimental signrpc walletrpc chainrpc invoicesrpc routerrpc backuprpc peerrpc submarineswaprpc breezbackuprpc"
RUN git clone https://github.com/lightningnetwork/lnd
ENV tags="experimental signrpc walletrpc chainrpc invoicesrpc routerrpc backuprpc peerrpc submarineswaprpc breezbackuprpc"
ENV DEV_TAGS="experimental signrpc walletrpc chainrpc invoicesrpc routerrpc backuprpc peerrpc submarineswaprpc breezbackuprpc"
RUN cd lnd && tags="experimental signrpc walletrpc chainrpc invoicesrpc routerrpc backuprpc peerrpc submarineswaprpc breezbackuprpc" && make install

COPY . /src/breez
RUN cd /src/breez && go build -tags="experimental signrpc walletrpc chainrpc invoicesrpc routerrpc backuprpc peerrpc submarineswaprpc breezbackuprpc" ./itest/client_node.go
VOLUME /root/.lnd
ENV LND_DIR=/root/.lnd
ENV GRPC_LISTEN_ADDRESS=0.0.0.0:50053
EXPOSE 10009 9735 50053
COPY ./docker/alice/start.sh .
RUN chmod +x ./start.sh
ENTRYPOINT ./start.sh
