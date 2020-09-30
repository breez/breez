FROM alpine:3.12 AS builder
RUN apk update
RUN apk add git go musl-dev make bash
RUN git clone https://github.com/breez/lnd -b zero-conf-debug

RUN cd lnd \
    && go build -tags=experimental,invoicesrpc,signrpc,autopilotrpc,experimental,submarineswaprpc,chanreservedynamic,routerrpc,walletrpc,chainrpc ./cmd/lnd/ \
    && go build -tags=experimental,invoicesrpc,signrpc,autopilotrpc,experimental,submarineswaprpc,chanreservedynamic,routerrpc,walletrpc,chainrpc ./cmd/lncli/

VOLUME /root/.lnd
EXPOSE 10013 9739

COPY ./docker/lnd/start.sh .
RUN chmod +x ./start.sh
ENTRYPOINT /start.sh