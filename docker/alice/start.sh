#!/bin/bash
for (( ; ; ))
do
  echo "restarting node"
  cp /root/.lnd/breez.conf /root/.lnd/lnd.conf /root
  rm -rf /root/.lnd/shutdown
  rm -rf /root/.lnd/data/graph/simnet/sphinxreplay.db
  if [ -f /root/.lnd/delete_node ]
    then
      echo "deleting node"
      rm -rf /root/.lnd/delete_node
      rm -rf /root/.lnd/restart
      rm -rf /root/.lnd/data
      rm -rf /root/.lnd/logs
      rm -rf /root/.lnd/backup.db
      rm -rf /root/.lnd/session_encryption.db
      rm -rf /root/.lnd/breez.db
    fi  
  exec /src/breez/client_node &
  while [ ! -f /root/.lnd/shutdown ]
    do
      sleep 1
    done
  echo "killing node"
  pid=$(ps -ef | grep client_node | grep -v grep | awk '{print $1}')
  kill -9 $pid
  wait $pid
  sleep 10
done