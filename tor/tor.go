// TODO(nochiel) Add useful commands to manage Tor to the developer panel.
// NB. We can use Tor ControlPort commands to change config or query Tor at runtime.
// Ref. https://gitweb.torproject.org/torspec.git/tree/control-spec.txt
package tor

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/breez/breez/data"
)

type TorConfig data.TorConfig

func (t *TorConfig) NewHttpClient() (*http.Client, error) {
	proxyAddress := fmt.Sprintf("socks5://127.0.0.1:%v", t.Socks)
	proxyUrl, err := url.Parse(proxyAddress)
	if err != nil {
		return nil, fmt.Errorf("NewHttpClient: %w", err)
	}

	tr := &http.Transport{
		Proxy: http.ProxyURL(proxyUrl),
		Dial: (&net.Dialer{
			Timeout: 30 * time.Second,
		}).Dial,
	}

	client := &http.Client{Transport: tr, Timeout: time.Second * 30}
	return client, nil
}
