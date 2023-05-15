#!/bin/bash
SCRIPTDIR=$(dirname $0)

protoc --go_out=$SCRIPTDIR --go_opt=paths=source_relative --go-grpc_out=$SCRIPTDIR --go-grpc_opt=paths=source_relative -I=$SCRIPTDIR $SCRIPTDIR/*.proto