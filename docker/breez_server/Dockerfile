FROM golang:latest AS builder
COPY ./docker/breez_server/.env .

COPY ./docker/breez_server/start.sh .
RUN chmod +x ./start.sh
RUN git clone https://github.com/breez/server
RUN cd server \
    && go build .
RUN go install github.com/joho/godotenv/cmd/godotenv@latest
RUN git clone https://github.com/breez/lnd -b breez-node
RUN cd lnd \
    && tags="signrpc walletrpc chainrpc invoicesrpc routerrpc backuprpc peerrpc submarineswaprpc breezbackuprpc" \
    && make install
COPY ./itest/generate_macaroon_hex.go .
RUN go build ./generate_macaroon_hex.go
RUN chmod +x ./generate_macaroon_hex
RUN go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
ENTRYPOINT ./start.sh
