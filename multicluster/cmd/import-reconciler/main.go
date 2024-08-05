package importreconciler

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	l5dApi "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	controllerK8s "github.com/linkerd/linkerd2/controller/k8s"
	imp "github.com/linkerd/linkerd2/multicluster/service-import-reconciler"
	servicemirror "github.com/linkerd/linkerd2/multicluster/service-mirror"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	l5dk8s "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
)

const (
	linkWatchRestartAfter = 10 * time.Second
	// Duration of the lease
	LEASE_DURATION = 30 * time.Second
	// Deadline for the leader to refresh its lease. Defaults to the same value
	// used by core controllers
	LEASE_RENEW_DEADLINE = 10 * time.Second
	// Duration leader elector clients should wait between action re-tries.
	// Defaults to the same value used by core controllers
	LEASE_RETRY_PERIOD = 2 * time.Second
)

var (
	clusterWatcher *servicemirror.RemoteClusterServiceWatcher
	probeWorker    *servicemirror.ProbeWorker
)

// Main executes the service-mirror controller
func Main(args []string) {
	cmd := flag.NewFlagSet("service-import-reconciler", flag.ExitOnError)

	kubeConfigPath := cmd.String("kubeconfig", "", "path to the local kube config")
	metricsAddr := cmd.String("metrics-addr", ":9999", "address to serve scrapable metrics on")
	namespace := cmd.String("namespace", "", "namespace containing Link and credentials Secret")
	enablePprof := cmd.Bool("enable-pprof", false, "Enable pprof endpoints on the admin server")

	flags.ConfigureAndParse(cmd, args)

	ready := false
	adminServer := admin.NewServer(*metricsAddr, *enablePprof, &ready)

	go func() {
		log.Infof("starting admin server on %s", *metricsAddr)
		if err := adminServer.ListenAndServe(); err != nil {
			log.Errorf("failed to start service mirror admin server: %s", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		<-stop
		log.Info("Received shutdown signal")
		// Cancel root context. Cancellation will be propagated to all other
		// contexts that are children of the root context.
		done <- struct{}{}
	}()

	controllerK8sAPI, err := controllerK8s.InitializeAPI(
		context.TODO(),
		*kubeConfigPath,
		false,
		"local",
		controllerK8s.Svc,
		controllerK8s.Smp,
		controllerK8s.Link,
	)
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	config, err := l5dk8s.GetConfig(*kubeConfigPath, "")
	if err != nil {
		panic(fmt.Sprintf("error reading Kubernetes config, %s", err))
	}

	l5dClient, err := l5dApi.NewForConfig(config)
	if err != nil {
		panic(fmt.Sprintf("error creating linkerd CRD client, %s", err))
	}

	controllerK8sAPI.Sync(nil)
	recon := imp.NewServiceImportWatcher(controllerK8sAPI, l5dClient, *namespace, "HOST123", done)
	recon.Run()
	log.Info("Shutting down")
}
