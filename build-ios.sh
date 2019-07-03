# GOPATH and ANDROID_HOME needs to be set
# gomobile & gobind needs to be installed in $GOPATH/bin

mkdir -p build/ios
PATH=$PATH:$GOPATH/bin gomobile bind -target=ios -tags="ios experimental signrpc walletrpc chainrpc invoicesrpc routerrpc backuprpc peerrpc submarineswaprpc breezbackuprpc" -o build/ios/bindings.framework github.com/breez/breez/bindings
