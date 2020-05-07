package srv

import (
	"net/http"
	"net/http/httputil"
	"strings"
)

// reverseProxy is an HTTP reverse proxy that forwards all web requests
// containing paths prefixed  to the corresponding service. The proxy
// strips the prefix and rewrites the Host header before sending.
type reverseProxy struct {
	*httputil.ReverseProxy
}

func newReverseProxy(addr string, prefix string) *reverseProxy {
	director := func(req *http.Request) {
		req.Host = addr
		req.URL.Host = addr
		req.URL.Scheme = "http"
		req.URL.Path = strings.TrimPrefix(req.URL.Path, prefix)

		// the default director implementation does this, so we will too
		if _, ok := req.Header["User-Agent"]; !ok {
			// explicitly disable User-Agent so it's not set to default value
			req.Header.Set("User-Agent", "")
		}
	}

	return &reverseProxy{
		ReverseProxy: &httputil.ReverseProxy{Director: director},
	}
}
