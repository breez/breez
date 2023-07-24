FROM golang:latest AS builder
COPY ./docker/lspd/.env.template .
COPY ./docker/lspd/start.sh .
RUN chmod +x ./start.sh
RUN git clone https://github.com/breez/lspd
RUN cd lspd \
    && go mod tidy \
    && go build .
RUN go install github.com/joho/godotenv/cmd/godotenv@latest

RUN git clone https://github.com/breez/lnd -b breez-node-v0.16.4-beta
RUN cd lnd \
    && tags="signrpc walletrpc chainrpc invoicesrpc routerrpc backuprpc peerrpc submarineswaprpc breezbackuprpc" \
    && make install
COPY . /src/breez
RUN cd /src/breez && go build ./itest/generate_lspd_config.go
RUN cp /src/breez/generate_lspd_config .
RUN chmod +x ./generate_lspd_config
RUN chmod +x ./lspd/lspd
RUN ./lspd/lspd genkey >> .env.template
RUN echo ' UPDATE public.new_channel_params set token='"'"'8qFbOxF8K8frgrhNE/Hq/UkUlq7A1Qvh8um1VdCUv2L4es/RXEe500E+FAKkLI4X'"';" >> ./lspd/postgresql/migrations/000012_new_channel_params_token.up.sql
RUN go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
#CMD /root/go/bin/godotenv ./server/server
ENTRYPOINT ./start.sh
