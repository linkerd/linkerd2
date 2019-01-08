package admin

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

type handler struct {
	promHandler http.Handler
}

// StartServer starts an admin server listening on a given address.
func StartServer(addr string) {
	log.Infof("starting admin server on %s", addr)

	h := &handler{
		promHandler: promhttp.Handler(),
	}

	s := &http.Server{
		Addr:         addr,
		Handler:      h,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Fatal(s.ListenAndServe())
}

func (h *handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.URL.Path {
	case "/metrics":
		h.promHandler.ServeHTTP(w, req)
	case "/ping":
		h.servePing(w, req)
	case "/ready":
		h.serveReady(w, req)
	default:
		http.NotFound(w, req)
	}
}

func (h *handler) servePing(w http.ResponseWriter, req *http.Request) {
	w.Write([]byte("pong\n"))
}

func (h *handler) serveReady(w http.ResponseWriter, req *http.Request) {
	w.Write([]byte("ok\n"))
}
