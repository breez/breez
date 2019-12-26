# breez
In order to build breez you will need to install [gomobile](https://github.com/golang/go/wiki/Mobile) and go 1.13.4.
## Prepare your environment
```
git clone https://github.com/breez/breez.git gopath/src/github.com/breez/breez
export GOPATH=$(pwd)/gopath
GO111MODULE=off go get golang.org/x/mobile/cmd/gomobile
GO111MODULE=off go get golang.org/x/mobile/cmd/gobind
export ANDROID_HOME=<your android sdk directory>
$GOPATH/bin/gomobile init
cd gopath/src/github.com/breez/breez
go mod vendor
```
## Building `breez` for Android
```
./build.sh
```
The file breez.aar will be built in build/android/
## Building `breez` for iOS
```
./build-ios.sh
```
The framework bindings.framework will be built in build/ios/
