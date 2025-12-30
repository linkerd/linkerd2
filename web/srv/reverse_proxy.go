package srv

import (
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/linkerd/linkerd2/pkg/protohttp"
)

// reverseProxy is an HTTP reverse proxy that forwards all web requests
// containing paths prefixed  to the corresponding service. The proxy
// strips the prefix and rewrites the Host header before sending.
type reverseProxy struct {
	*httputil.ReverseProxy
}

func newReverseProxy(addr string, prefix string) *reverseProxy {
	director := func(req *http.Request) {
		req.URL.Host = addr
		req.URL.Scheme = "http"
		req.URL.Path = strings.TrimPrefix(req.URL.Path, prefix)

		// the default director implementation does this, so we will too
		if _, ok := req.Header["User-Agent"]; !ok {
			// explicitly disable User-Agent so it's not set to default value
			req.Header.Set("User-Agent", "")
		}
	}

	// ModifyResponse adds the Via header to responses to identify that
	// this response was proxied through Linkerd
	modifyResponse := func(resp *http.Response) error {
		resp.Header.Set(protohttp.ViaHeaderName, protohttp.ViaHeaderValue)
		return nil
	}

	return &reverseProxy{
		ReverseProxy: &httputil.ReverseProxy{
			Director:       director,
			ModifyResponse: modifyResponse,
		},
	}
}
