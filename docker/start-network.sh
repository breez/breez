# create alice folder
rm -rf alice_node
mkdir alice_node
cp ./alice/lnd.conf ./alice/breez.conf alice_node 

# create breez node folder
rm -rf breez_node
mkdir breez_node
cp ./breez/lnd.conf breez_node

# run breez node and get mining address
docker-compose -f simnet.yml run -d --name breez breez
sleep 7
docker exec -i -t breez "/root/go/bin/lncli" -network=simnet newaddress np2wkh | jq -r '.address'
export MINING_ADDRESS=$(docker exec -i -t breez "/root/go/bin/lncli" -network=simnet newaddress np2wkh | jq -r '.address')

docker exec -i -t breez "/root/go/bin/lncli" -network=simnet getinfo 
#| jq -r '.pubkey'
export NODE_PUBKEY=$(docker exec -i -t breez "/root/go/bin/lncli" -network=simnet newaddress np2wkh | jq -r '.address')

# restart containers
docker-compose -f simnet.yml down
docker-compose -f simnet.yml up

#generate 400 blocks
sleep 5
docker exec -it btcd /start-btcctl.sh generate 400