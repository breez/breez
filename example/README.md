# This can be run to test the breez library.

Make sure you define the `LND_DIR` envioment variable and place both `breez.conf` and `lnd.conf` there.

```
export LND_DIR=/Users/<YOU>/tmp
cp breez.conf $LND_DIR
cp lnd.conf $LND_DIR
```

In order to build run `go build .` from this (**example**) folder.

run the generated `example` file.
```
./example
```
