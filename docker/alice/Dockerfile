FROM alpine:3.12 AS builder
RUN apk update
RUN apk add git go musl-dev make

RUN git clone https://github.com/lightningnetwork/lnd
RUN cd lnd && make install

COPY . /src/breez
RUN cd /src/breez && go build -tags="experimental signrpc walletrpc chainrpc invoicesrpc routerrpc backuprpc peerrpc submarineswaprpc breezbackuprpc" ./itest/client_node.go
VOLUME /root/.lnd
ENV LND_DIR=/root/.lnd
EXPOSE 10009 9735
CMD /src/breez/client_node 