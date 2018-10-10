# GOPATH and ANDROID_HOME needs to be set
# gomobile & gobind needs to be installed in $GOPATH/bin

mkdir -p build/android
PATH=$PATH:$GOPATH/bin gomobile bind -target=android -tags="android" -o build/android/breez.aar github.com/breez/breez/bindings
