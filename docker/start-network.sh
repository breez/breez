export ALICE_BREEZ_ADDRESS="127.0.0.1:50053"
export ALICE_DIR=$TEST_DIR/alice_node
export ALICE_LND_ADDRESS="127.0.0.1:10009"
export BOB_DIR=$TEST_DIR/bob_node
export BOB_LND_ADDRESS="127.0.0.1:10011"
export BOB_BREEZ_ADDRESS="127.0.0.1:50054"
export LND_NODE_DIR=$TEST_DIR/lnd
export LND_NODE_ADDRESS="127.0.0.1:10013"
export BREEZ_DIR=$TEST_DIR/breez_node
export BREEZ_LND_ADDRESS="127.0.0.1:10010"
export SUBSWAP_DIR=$TEST_DIR/subswap_node
export SUBSWAP_LND_ADDRESS="127.0.0.1:10012"
export BTCD_HOST="127.0.0.1:18556"
export BTCD_CERT_FILE=$TEST_DIR/btcd-rpc.cert

docker-compose -f simnet.yml down --remove-orphans

rm -rf $TEST_DIR
mkdir $TEST_DIR

# create alice folder
mkdir $ALICE_DIR
cp ./alice/lnd.conf ./alice/breez.conf $ALICE_DIR

# create bob folder
mkdir $BOB_DIR
cp ./alice/lnd.conf ./alice/breez.conf $BOB_DIR

# create lnd node folder
mkdir $LND_NODE_DIR
cp ./lnd/lnd.conf $LND_NODE_DIR

# create breez node folder
mkdir $BREEZ_DIR
cp ./breez/lnd.conf $BREEZ_DIR

# create subswap node folder
mkdir $SUBSWAP_DIR
cp ./breez/lnd.conf $SUBSWAP_DIR

# bootstrap minning address so we can generate blocks
export MINING_ADDRESS=SVv2gHztnPJa2YU7J4SjkiBMXcTvnDWxgM

# generate some blocks
docker-compose -f simnet.yml up -d btcd
sleep 2
docker exec btcd /start-btcctl.sh generate 400

# run breez node and get mining address
docker-compose -f simnet.yml up -d breez

# waiting for breez node to be ready
until docker exec breez "cat" /root/.lnd/logs/bitcoin/simnet/lnd.log | grep 'Starting sub RPC server: InvoicesRPC' > /dev/null;
do
    sleep 1
done

# generate mining address for breez node
export MINING_ADDRESS=$(docker exec breez "/lnd/lncli" -network=simnet newaddress np2wkh | jq -r '.address')
echo $MINING_ADDRESS
docker exec btcd cat /rpc/rpc.cert > $TEST_DIR/btcd-rpc.cert

# export the lspd node pubkey
export NODE_PUBKEY=$(docker exec breez "/lnd/lncli" -network=simnet getinfo | jq -r '.identity_pubkey')
echo "NODE_PUBKEY=$NODE_PUBKEY"

# restart containers because we need now btcd to use the new mining address
docker-compose -f simnet.yml down
docker-compose -f simnet.yml up -d postgres
docker-compose -f simnet.yml up -d --no-recreate postgres_interceptor
docker-compose -f simnet.yml up -d --no-recreate breez

# waiting for breez node to be ready so lspd won't crash
until docker exec breez "cat" /root/.lnd/logs/bitcoin/simnet/lnd.log | grep 'Starting sub RPC server: InvoicesRPC' > /dev/null;
do
    sleep 1    
done
docker-compose -f simnet.yml up -d --no-recreate

# waiting for breez server to start and be ready
until docker logs breez_server | grep 'Breez server ready!' > /dev/null;
do
    sleep 1    
done

# waiting for subswap node to start and be ready
until docker exec subswap_node "cat" /root/.lnd/logs/bitcoin/simnet/lnd.log | grep 'Starting sub RPC server: InvoicesRPC' > /dev/null;
do
    sleep 1    
done

docker exec -it postgres_breez_server psql -h 0.0.0.0 -U postgres -c "insert into api_keys (api_key, lsp_ids, api_user) values ('8qFbOxF8K8frgrhNE/Hq/UkUlq7A1Qvh8um1VdCUv2L4es/RXEe500E+FAKkLI4X',json_build_array('lspd-secret'),'test')"
docker exec btcd /start-btcctl.sh generate 400
go test -v ../itest/tests 