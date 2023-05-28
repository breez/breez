/go/bin/migrate   -source file:///go/lspd/postgresql/migrations/ --database postgres://postgres:test@10.5.0.8:5432/postgres?sslmode=disable up 100

echo "MEMPOOL_API_BASE_URL=$MEMPOOL_API_BASE_URL"
echo "NODE_PUBKEY=$NODE_PUBKEY"
echo "NODE_HOST=$NODE_HOST"
./generate_lspd_config 10.5.0.3:10009 /root/breez_node/data/chain/bitcoin/simnet/admin.macaroon /root/breez_node/tls.cert $NODE_PUBKEY $NODE_HOST >> .env
cat .env

exec /go/bin/godotenv ./lspd/lspd