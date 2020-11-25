package webhook

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/admin"
	pkgk8s "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
)

// Launch sets up and starts the webhook and metrics servers
func Launch(
	ctx context.Context,
	APIResources []k8s.APIResource,
	handler Handler,
	component,
	metricsAddr string,
	addr string,
	kubeconfig string,
) {
	stop := make(chan os.Signal, 1)
	defer close(stop)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	k8sAPI, err := k8s.InitializeAPI(ctx, kubeconfig, false, APIResources...)
	if err != nil {
		log.Fatalf("failed to initialize Kubernetes API: %s", err)
	}

	s, err := NewServer(ctx, k8sAPI, addr, pkgk8s.MountPathTLSBase, handler, component)
	if err != nil {
		log.Fatalf("failed to initialize the webhook server: %s", err)
	}

	k8sAPI.Sync(nil)

	go s.Start()
	go admin.StartServer(metricsAddr)

	<-stop
	log.Info("shutting down webhook server")
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		log.Error(err)
	}
}
