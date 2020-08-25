sleep 20s
./generate_macaroon_hex LND /root/breez_node/data/chain/bitcoin/simnet/admin.macaroon /root/breez_node/tls.cert >> .env
cat .env
/go/bin/migrate   -source file:///go/lspd/postgresql/migrations/ --database postgres://postgres:test@10.5.0.8:5432/postgres?sslmode=disable up 10
#wait for breez rpc
until cat /root/breez_node/logs/bitcoin/simnet/lnd.log | grep 'RPC server listening on' > /dev/null;
do
    sleep 1
    echo "lspd waiting for breez RPC..."
done
exec /go/bin/godotenv ./lspd/lspd