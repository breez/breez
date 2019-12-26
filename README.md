# breez
In order to build breez you will need to install [gomobile](https://github.com/golang/go/wiki/Mobile) and go 1.13.4.
## Prepare your environment
```
git clone https://github.com/breez/breez.git gopath/src/github.com/breez/breez
export GOPATH=$(pwd)/gopath
GO111MODULE=off go get golang.org/x/mobile/cmd/gomobile
GO111MODULE=off go get golang.org/x/mobile/cmd/gobind
$GOPATH/bin/gomobile init
cd gopath/src/github.com/breez/breez
go mod vendor
```
## Building `breez` for Android
You need to install the ndk as part of your sdk Tools.
If you have a separate ndk installed then make sure to set the ANDROID_NDK_HOME environment variable to your ndk install location.
```
export ANDROID_HOME=<your android sdk directory>
```
Or in case you want to use a direct ndk path
```
export ANDROID_NDK_HOME=<your android ndk directory>
```
Then you ready to run the build:
```
./build.sh
```
The file breez.aar will be built in build/android/
## Building `breez` for iOS
```
./build-ios.sh
```
The bindings.framework will be built in build/ios/
