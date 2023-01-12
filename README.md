# breez
In order to build breez you will need to install [gomobile](https://github.com/golang/go/wiki/Mobile) and [go 1.16.x](https://go.dev/dl/). If you install go from homebrew, you will have to ensure the GOPATH environment variable is set yourself.
## Prepare your environment
```
git clone https://github.com/breez/breez.git
go get -d golang.org/x/mobile/cmd/gomobile
go get -d golang.org/x/mobile/cmd/gobind
export PATH=$PATH:$GOPATH/bin
gomobile init
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
If you are using NDK 24+, install [NDK r19](https://github.com/android/ndk/wiki/Unsupported-Downloads#r19c) and point ANDROID_NDK_HOME to it's folder due to [gomobile incompatibility](https://github.com/golang/go/issues/35030). Alternatively, you can add [-androidapi 19](https://github.com/golang/go/issues/52470#issuecomment-1203998993) to gomobile command in build script.

If the library will be run on an emulator target, add `-ldflags=-extldflags=-Wl,-soname,libgojni.so` to gomobile command in build script.

Then you are ready to run the build:
```
./build.sh
```
The file breez.aar will be built in build/android/
## Building `breez` for iOS
```
./build-ios.sh
```
The bindings.framework will be built in build/ios/
