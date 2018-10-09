# GOPATH and ANDROID_HOME needs to be set
# gomobile & gobind needs to be installed in $GOPATH/bin

mkdir -p build/android
PATH=$PATH:$GOPATH/bin ANDROID_HOME=~/devel/android/android-sdk-linux/ gomobile bind -target=android -tags="android" -o build/android/breez.aar github.com/breez/breez/bindings
