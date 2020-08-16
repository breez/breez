sleep 20s
./generate_macaroon_hex /root/breez_node/data/chain/bitcoin/simnet/admin.macaroon /root/breez_node/tls.cert >> .env
cat .env
/root/go/bin/migrate   -source file:///server/postgresql/migrations/ --database postgres://postgres:test@10.5.0.6:5432/postgres?sslmode=disable up 10
exec /root/go/bin/godotenv ./server/server