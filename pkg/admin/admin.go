package admin

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type handler struct {
	promHandler http.Handler
}

// NewServer returns an initialized `http.Server`, configured to listen on an address.
func NewServer(addr string) *http.Server {
	h := &handler{
		promHandler: promhttp.Handler(),
	}

	return &http.Server{
		Addr:    addr,
		Handler: h,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	debugPathPrefix := "/debug/pprof/"
	switch req.URL.Path {
	case "/metrics":
		h.promHandler.ServeHTTP(w, req)
	case "/ping":
		h.servePing(w)
	case "/ready":
		h.serveReady(w)
	case fmt.Sprintf("%scmdline", debugPathPrefix):
		pprof.Cmdline(w, req)
	case fmt.Sprintf("%sprofile", debugPathPrefix):
		pprof.Profile(w, req)
	case fmt.Sprintf("%strace", debugPathPrefix):
		pprof.Trace(w, req)
	case fmt.Sprintf("%ssymbol", debugPathPrefix):
		pprof.Symbol(w, req)
	default:
		if strings.HasPrefix(req.URL.Path, "/debug/pprof/") {
			pprof.Index(w, req)
		} else {
			http.NotFound(w, req)
		}
	}
}

func (h *handler) servePing(w http.ResponseWriter) {
	w.Write([]byte("pong\n"))
}

func (h *handler) serveReady(w http.ResponseWriter) {
	w.Write([]byte("ok\n"))
}
