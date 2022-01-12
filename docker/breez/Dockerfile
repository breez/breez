FROM alpine:3.12 AS builder
RUN apk update
RUN apk add git go musl-dev make
RUN git clone https://github.com/breez/lnd -b fix-subswapper-macaroon

RUN cd lnd \
    && go build -tags=experimental,invoicesrpc,signrpc,autopilotrpc,experimental,submarineswaprpc,chanreservedynamic,routerrpc,walletrpc,chainrpc ./cmd/lnd/ \
    && go build -tags=experimental,invoicesrpc,signrpc,autopilotrpc,experimental,submarineswaprpc,chanreservedynamic,routerrpc,walletrpc,chainrpc ./cmd/lncli/

VOLUME /root/.lnd
EXPOSE 10009 9735
CMD /lnd/lnd