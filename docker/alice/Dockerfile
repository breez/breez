FROM alpine:3.12 AS builder
RUN apk update
RUN apk add git go musl-dev make bash

RUN git clone https://github.com/lightningnetwork/lnd
RUN cd lnd && make install

COPY . /src/breez
RUN cd /src/breez && go build -tags="experimental signrpc walletrpc chainrpc invoicesrpc routerrpc backuprpc peerrpc submarineswaprpc breezbackuprpc" ./itest/client_node.go
VOLUME /root/.lnd
ENV LND_DIR=/root/.lnd
ENV GRPC_LISTEN_ADDRESS=0.0.0.0:50053
EXPOSE 10009 9735 50053
COPY ./docker/alice/start.sh .
RUN chmod +x ./start.sh
ENTRYPOINT ./start.sh