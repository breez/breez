FROM golang:latest AS builder
COPY ./docker/lspd/.env .
COPY ./docker/lspd/start.sh .
RUN chmod +x ./start.sh
RUN git clone https://github.com/breez/lspd
RUN cd lspd \
    && go mod tidy \
    && go build .
RUN go install github.com/joho/godotenv/cmd/godotenv@latest

RUN git clone https://github.com/breez/lnd -b breez-node
RUN cd lnd \
    && tags="signrpc walletrpc chainrpc invoicesrpc routerrpc backuprpc peerrpc submarineswaprpc breezbackuprpc" \
    && make install
COPY . /src/breez
RUN cd /src/breez && go build ./itest/generate_lspd_config.go
RUN cp /src/breez/generate_lspd_config .
RUN chmod +x ./generate_lspd_config
RUN chmod +x ./lspd/lspd
RUN ./lspd/lspd genkey >> .env

RUN go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
#CMD /root/go/bin/godotenv ./server/server
ENTRYPOINT ./start.sh
