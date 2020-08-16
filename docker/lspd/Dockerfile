FROM golang:1.14-alpine3.12 AS builder
RUN apk update
RUN apk add git go musl-dev make
COPY ./docker/lspd/.env .
COPY ./docker/lspd/start.sh .
RUN chmod +x ./start.sh
RUN git clone https://github.com/breez/lspd
RUN cd lspd \
    && go build .
RUN go get github.com/joho/godotenv/cmd/godotenv
RUN git clone https://github.com/breez/lnd -b breez-node-v0.10.0-beta-htlc-interceptor-zeroconf
RUN cd lnd \
    && tage="signrpc walletrpc chainrpc invoicesrpc routerrpc backuprpc peerrpc submarineswaprpc breezbackuprpc" \
    && make install
COPY ./itest/generate_macaroon_hex.go .
RUN go build ./generate_macaroon_hex.go
RUN chmod +x ./generate_macaroon_hex
RUN chmod +x ./lspd/lspd
RUN ./lspd/lspd genkey >> .env

RUN go get -tags 'postgres' -u github.com/golang-migrate/migrate/cmd/migrate
#CMD /root/go/bin/godotenv ./server/server
ENTRYPOINT ./start.sh