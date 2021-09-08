// TODO(nochiel) Add useful commands to manage Tor to the developer panel.
// NB. We can use Tor ControlPort commands to change config or query Tor at runtime.
// Ref. https://gitweb.torproject.org/torspec.git/tree/control-spec.txt
package tor

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/breez/breez/data"
)

type TorConfig data.TorConfig

func (t *TorConfig) NewHttpClient() (*http.Client, error) {

	httpAddress := fmt.Sprintf("http://127.0.0.1:%v", t.Http)
	proxyUrl, err := url.Parse(httpAddress)
	if err != nil {
		return nil, fmt.Errorf("NewHttpClient: %w", err)
	}

	tr := &http.Transport{}
	tr.Proxy = func(r *http.Request) (result *url.URL, err error) {
		return proxyUrl, nil
	}

	client := &http.Client{Transport: tr}
	return client, nil
}
