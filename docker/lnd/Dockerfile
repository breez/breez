FROM alpine:latest AS builder
RUN apk update
RUN apk add git go musl-dev make bash
RUN git clone --depth 1 --branch v0.16.4-beta https://github.com/lightningnetwork/lnd

RUN cd lnd \
    && go build -tags=experimental,invoicesrpc,signrpc,autopilotrpc,routerrpc,walletrpc,chainrpc ./cmd/lnd/ \
    && go build -tags=experimental,invoicesrpc,signrpc,autopilotrpc,routerrpc,walletrpc,chainrpc ./cmd/lncli/

VOLUME /root/.lnd
EXPOSE 10013 9739

COPY ./docker/lnd/start.sh .
RUN chmod +x ./start.sh
ENTRYPOINT /start.sh