export ALICE_BREEZ_ADDRESS="127.0.0.1:50053"
export ALICE_DIR=$TEST_DIR/alice_node
export ALICE_LND_ADDRESS="127.0.0.1:10009"
export BOB_DIR=$TEST_DIR/bob_node
export BOB_LND_ADDRESS="127.0.0.1:10011"
export BOB_BREEZ_ADDRESS="127.0.0.1:50054"
export BREEZ_DIR=$TEST_DIR/breez_node
export BREEZ_LND_ADDRESS="127.0.0.1:10010"
export SUBSWAP_DIR=$TEST_DIR/subswap_node
export SUBSWAP_LND_ADDRESS="127.0.0.1:10012"
export BTCD_HOST="127.0.0.1:18556"
export BTCD_CERT_FILE=$TEST_DIR/btcd-rpc.cert

rm -rf $TEST_DIR
mkdir $TEST_DIR

# create alice folder
mkdir $ALICE_DIR
cp ./alice/lnd.conf ./alice/breez.conf $ALICE_DIR

# create bob folder
mkdir $BOB_DIR
cp ./alice/lnd.conf ./alice/breez.conf $BOB_DIR

# create breez node folder
mkdir $BREEZ_DIR
cp ./breez/lnd.conf $BREEZ_DIR

# create subswap node folder
mkdir $SUBSWAP_DIR
cp ./breez/lnd.conf $SUBSWAP_DIR

# run breez node and get mining address
docker-compose -f simnet.yml run -d --name breez breez

#wait for breez rpc
until docker exec -i -t breez "cat" /root/.lnd/logs/bitcoin/simnet/lnd.log | grep 'RPC server listening on' > /dev/null;
do
    sleep 1
    echo "waiting for breez RPC..."
done
docker exec -i -t breez "/lnd/lncli" -network=simnet newaddress np2wkh | jq -r '.address'
export MINING_ADDRESS=$(docker exec -i -t breez "/lnd/lncli" -network=simnet newaddress np2wkh | jq -r '.address')
docker exec -it btcd cat /rpc/rpc.cert > $TEST_DIR/btcd-rpc.cert

# restart containers
docker-compose -f simnet.yml down
docker-compose -f simnet.yml up -d
docker exec -it btcd /start-btcctl.sh generate 400

#go test ../itest/tests