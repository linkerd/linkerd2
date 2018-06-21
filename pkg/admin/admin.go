package admin

import (
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

type handler struct {
	promHandler http.Handler
	ready       bool
	sync.RWMutex
}

func StartServer(addr string, readyCh <-chan struct{}) {
	log.Infof("starting admin server on %s", addr)

	h := &handler{
		promHandler: promhttp.Handler(),
		ready:       readyCh == nil,
	}

	if readyCh != nil {
		go func() {
			<-readyCh
			h.setReady(true)
		}()
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
	if h.getReady() {
		w.Write([]byte("ok\n"))
	} else {
		http.Error(w, "unready", http.StatusServiceUnavailable)
	}
}

func (h *handler) getReady() bool {
	h.RLock()
	defer h.RUnlock()
	return h.ready
}

func (h *handler) setReady(ready bool) {
	h.Lock()
	defer h.Unlock()
	h.ready = ready
}
