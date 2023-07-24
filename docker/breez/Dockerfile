FROM alpine:latest AS builder
RUN apk update
RUN apk add git go musl-dev make
RUN git clone --depth 1 --branch breez-node-v0.16.4-beta https://github.com/breez/lnd

RUN cd lnd \
    && go build -tags=experimental,invoicesrpc,signrpc,autopilotrpc,experimental,submarineswaprpc,chanreservedynamic,routerrpc,walletrpc,chainrpc ./cmd/lnd/ \
    && go build -tags=experimental,invoicesrpc,signrpc,autopilotrpc,experimental,submarineswaprpc,chanreservedynamic,routerrpc,walletrpc,chainrpc ./cmd/lncli/

VOLUME /root/.lnd
EXPOSE 10009 9735
CMD /lnd/lnd