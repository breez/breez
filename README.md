# breez
## Build
```
git clone github.com/breez/breez gopath/src/github.com/breez/breez
export GOPATH=$(pwd)/gopath
go get golang.org/x/mobile/cmd/gomobile
go get golang.org/x/mobile/cmd/gobind
export ANDROID_HOME=<your android sdk directory>
$GOPATH/bin/gomobile init
cd gopath/src/github.com/breez/breez
./build.sh
```
The file breez.aar will be built in build/android/
