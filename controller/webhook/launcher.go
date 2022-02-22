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

	go s.Start()

	k8sAPI.Sync(nil)

	adminServer := admin.NewServer(metricsAddr)

	go func() {
		log.Infof("starting admin server on %s", metricsAddr)
		if err = adminServer.ListenAndServe(); err != nil {
			log.Fatalf("Failed to start admin server: %s", err)
		}
	}()

	<-stop
	log.Info("shutting down webhook server")
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		log.Error(err)
	}

	if err = adminServer.Shutdown(ctx); err != nil {
		log.Fatalf("Failed to gracefully shutdown controller webhook: %s", err)
	}
}
