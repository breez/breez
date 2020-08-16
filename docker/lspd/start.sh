sleep 20s
./generate_macaroon_hex /root/breez_node/data/chain/bitcoin/simnet/admin.macaroon /root/breez_node/tls.cert >> .env
cat .env
/go/bin/migrate   -source file:///go/lspd/postgresql/migrations/ --database postgres://postgres:test@10.5.0.8:5432/postgres?sslmode=disable up 10
exec /go/bin/godotenv ./lspd/lspd