package servicemirror

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/linkerd/linkerd2/controller/k8s"
	pkgk8s "github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type gatewayStats struct {
	Alive            bool   `json:"alive"`
	Latency          string `json:"latency"`
	NumberOfServices int    `json:"numberOfServices"`
}

// GatewayStatus is the status of a gateway.
type GatewayStatus struct {
	alive   bool
	latency uint64
}

type handler struct {
	k8sAPI        *k8s.API
	clusterName   string
	gatewayStatus GatewayStatus
}

// NewServer returns a new instance of the service mirror server.
//
// The service mirror server serves stats about its gateway via the
// /gateway.json endpoint.
func NewServer(addr string, k8sAPI *k8s.API, clusterNameUpdates chan string, gatewayUpdates chan GatewayStatus) *http.Server {
	handler := &handler{
		k8sAPI: k8sAPI,
	}

	go func() {
		for {
			select {
			case name := <-clusterNameUpdates:
				handler.clusterName = name
			case status := <-gatewayUpdates:
				handler.gatewayStatus = status
			}
		}
	}()

	return &http.Server{
		Addr:    addr,
		Handler: handler,
	}
}

func (handler *handler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/gateway.json":
		handler.gateway(w)
	default:
		http.NotFound(w, request)
	}
}

func (handler *handler) gateway(w http.ResponseWriter) {
	// Multiple service mirrors can be mirroring services on a cluster, so we
	// ensure through selector that we are only selecting mirror services from
	// this service mirror's target cluster.
	selector := fmt.Sprintf("%s=%s,%s=%s",
		pkgk8s.MirroredResourceLabel, "true",
		pkgk8s.RemoteClusterNameLabel, handler.clusterName,
	)
	services, err := handler.k8sAPI.Client.CoreV1().Services(corev1.NamespaceAll).List(context.Background(), metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get services: %s", err)
		return
	}
	stats := gatewayStats{
		Alive:            handler.gatewayStatus.alive,
		Latency:          fmt.Sprintf("%dms", handler.gatewayStatus.latency),
		NumberOfServices: len(services.Items),
	}
	out, err := json.MarshalIndent(stats, "", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshalling JSON: %s\n", err)
		return
	}
	w.Write([]byte(fmt.Sprintf("%s\n", out)))
}
