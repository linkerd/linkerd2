package admin

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type handler struct {
	promHandler http.Handler
	enablePprof bool
}

// NewServer returns an initialized `http.Server`, configured to listen on an address.
func NewServer(addr string, enablePprof bool) *http.Server {
	h := &handler{
		promHandler: promhttp.Handler(),
		enablePprof: enablePprof,
	}

	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 15 * time.Second,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	debugPathPrefix := "/debug/pprof/"
	if h.enablePprof && strings.HasPrefix(req.URL.Path, debugPathPrefix) {
		switch req.URL.Path {
		case fmt.Sprintf("%scmdline", debugPathPrefix):
			pprof.Cmdline(w, req)
		case fmt.Sprintf("%sprofile", debugPathPrefix):
			pprof.Profile(w, req)
		case fmt.Sprintf("%strace", debugPathPrefix):
			pprof.Trace(w, req)
		case fmt.Sprintf("%ssymbol", debugPathPrefix):
			pprof.Symbol(w, req)
		default:
			pprof.Index(w, req)
		}
		return
	}
	switch req.URL.Path {
	case "/metrics":
		h.promHandler.ServeHTTP(w, req)
	case "/ping":
		h.servePing(w)
	case "/ready":
		h.serveReady(w)
	default:
		http.NotFound(w, req)
	}
}

func (h *handler) servePing(w http.ResponseWriter) {
	w.Write([]byte("pong\n"))
}

func (h *handler) serveReady(w http.ResponseWriter) {
	w.Write([]byte("ok\n"))
}
