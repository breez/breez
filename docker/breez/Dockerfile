FROM alpine:3.12 AS builder
RUN apk update
RUN apk add git go musl-dev make
RUN git clone https://github.com/breez/lnd -b breez-node-v0.10.0-beta-htlc-interceptor-zeroconf
RUN cd lnd \
    && tage="signrpc walletrpc chainrpc invoicesrpc routerrpc backuprpc peerrpc submarineswaprpc breezbackuprpc" \
    && make install

VOLUME /root/.lnd
EXPOSE 10009 9735
CMD /root/go/bin/lnd