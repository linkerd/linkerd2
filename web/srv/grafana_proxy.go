package srv

import (
	"net/http"
	"net/http/httputil"
	"strings"
)

// grafanaProxy is an HTTP reverse proxy that forwards all web requests
// containing paths prefixed with "/grafana" to the grafana service. The proxy
// strips the "/grafana" prefix and rewrites the Host header before sending.
type grafanaProxy struct {
	*httputil.ReverseProxy
}

func newGrafanaProxy(addr string) *grafanaProxy {
	director := func(req *http.Request) {
		req.Host = addr
		req.URL.Host = addr
		req.URL.Scheme = "http"
		req.URL.Path = strings.TrimPrefix(req.URL.Path, "/grafana")

		// the default director implementation does this, so we will too
		if _, ok := req.Header["User-Agent"]; !ok {
			// explicitly disable User-Agent so it's not set to default value
			req.Header.Set("User-Agent", "")
		}
	}

	return &grafanaProxy{
		ReverseProxy: &httputil.ReverseProxy{Director: director},
	}
}
