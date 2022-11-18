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
	apiresources []k8s.APIResource,
	handler Handler,
	component,
	metricsAddr string,
	addr string,
	kubeconfig string,
	enablePprof bool,
) {
	stop := make(chan os.Signal, 1)
	defer close(stop)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	config, err := pkgk8s.GetConfig(kubeconfig, "")
	if err != nil {
		//nolint:gocritic
		log.Fatalf("error building Kubernetes API config: %s", err)
	}

	k8sAPI, err := pkgk8s.NewAPIForConfig(config, "", []string{}, 0)
	if err != nil {
		//nolint:gocritic
		log.Fatalf("error configuring Kubernetes API client: %s", err)
	}

	metadataAPI, err := k8s.InitializeMetadataAPI(kubeconfig, apiresources...)
	if err != nil {
		//nolint:gocritic
		log.Fatalf("failed to initialize Kubernetes API: %s", err)
	}

	s, err := NewServer(ctx, k8sAPI, metadataAPI, addr, pkgk8s.MountPathTLSBase, handler, component)
	if err != nil {
		//nolint:gocritic
		log.Fatalf("failed to initialize the webhook server: %s", err)
	}

	go s.Start()

	metadataAPI.Sync(nil)

	adminServer := admin.NewServer(metricsAddr, enablePprof)

	go func() {
		log.Infof("starting admin server on %s", metricsAddr)
		if err := adminServer.ListenAndServe(); err != nil {
			log.Errorf("failed to start webhook admin server: %s", err)
		}
	}()

	<-stop
	log.Info("shutting down webhook server")
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		log.Error(err)
	}

	adminServer.Shutdown(ctx)
}
