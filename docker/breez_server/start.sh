./generate_macaroon_hex LND /root/breez_node/data/chain/bitcoin/simnet/admin.macaroon /root/breez_node/tls.cert >> .env
./generate_macaroon_hex SUBSWAPPER_LND /root/subswap_node/data/chain/bitcoin/simnet/admin.macaroon /root/subswap_node/tls.cert >> .env
cat .env
/go/bin/migrate   -source file:///go/server/postgresql/migrations/ --database postgres://postgres:test@10.5.0.6:5432/postgres?sslmode=disable up 10

echo "Breez server ready!"
exec /go/bin/godotenv ./server/server
