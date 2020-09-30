#!/bin/bash
for (( ; ; ))
do
  echo "restarting node"
  cp /root/.lnd/lnd.conf /root
  rm -rf /root/.lnd/shutdown
  rm -rf /root/.lnd/data
  rm -rf /root/.lnd/logs
  rm -rf /root/.lnd/backup.db
  rm -rf /root/.lnd/session_encryption.db
  rm -rf /root/.lnd/breez.db
  exec /lnd/lnd &
  while [ ! -f /root/.lnd/shutdown ]
    do
      sleep 1
    done
    echo "killing node"
   pid=$(ps -ef | grep lnd | grep -v grep | awk '{print $1}')
   kill -9 $pid
   wait $pid
done