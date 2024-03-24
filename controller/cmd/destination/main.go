package destination

import (
	"context"
	"errors"
	"flag"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/linkerd/linkerd2/controller/api/destination"
	externalworkload "github.com/linkerd/linkerd2/controller/api/destination/external-workload"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/trace"
	"github.com/linkerd/linkerd2/pkg/util"
	log "github.com/sirupsen/logrus"
)

// Main executes the destination subcommand
func Main(args []string) {
	cmd := flag.NewFlagSet("destination", flag.ExitOnError)

	addr := cmd.String("addr", ":8086", "address to serve on")
	metricsAddr := cmd.String("metrics-addr", ":9996", "address to serve scrapable metrics on")
	kubeConfigPath := cmd.String("kubeconfig", "", "path to kube config")
	controllerNamespace := cmd.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	enableH2Upgrade := cmd.Bool("enable-h2-upgrade", true,
		"Enable transparently upgraded HTTP2 connections among pods in the service mesh")
	enableEndpointSlices := cmd.Bool("enable-endpoint-slices", true,
		"Enable the usage of EndpointSlice informers and resources")
	trustDomain := cmd.String("identity-trust-domain", "", "configures the name suffix used for identities")
	clusterDomain := cmd.String("cluster-domain", "", "kubernetes cluster domain")
	defaultOpaquePorts := cmd.String("default-opaque-ports", "", "configures the default opaque ports")
	enablePprof := cmd.Bool("enable-pprof", false, "Enable pprof endpoints on the admin server")
	// This will default to true. It can be overridden with experimental CLI
	// flags. Currently not exposed as a configuration value through Helm.
	exportControllerQueueMetrics := cmd.Bool("export-queue-metrics", true, "Exports queue metrics for the external workload controller")

	traceCollector := flags.AddTraceFlags(cmd)

	// Zone weighting is disabled by default because it is not consumed by
	// proxies. This feature exists to support experimentation on top of the
	// Linkerd control plane API.
	extEndpointZoneWeights := cmd.Bool("ext-endpoint-zone-weights", false,
		"Enable setting endpoint weighting based on zone locality")

	flags.ConfigureAndParse(cmd, args)

	ready := false
	adminServer := admin.NewServer(*metricsAddr, *enablePprof, &ready)

	go func() {
		log.Infof("starting admin server on %s", *metricsAddr)
		if err := adminServer.ListenAndServe(); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				log.Infof("Admin server closed (%s)", *metricsAddr)
			} else {
				log.Errorf("Admin server error (%s): %s", *metricsAddr, err)
			}
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	done := make(chan struct{})

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %s", *addr, err)
	}

	if *trustDomain == "" {
		*trustDomain = "cluster.local"
		log.Warnf(" expected trust domain through args (falling back to %s)", *trustDomain)
	}

	if *clusterDomain == "" {
		*clusterDomain = "cluster.local"
		log.Warnf("expected cluster domain through args (falling back to %s)", *clusterDomain)
	}

	opaquePorts := util.ParsePorts(*defaultOpaquePorts)

	log.Infof("Using default opaque ports: %v", opaquePorts)

	if *traceCollector != "" {
		if err := trace.InitializeTracing("linkerd-destination", *traceCollector); err != nil {
			log.Warnf("failed to initialize tracing: %s", err)
		}
	}

	// we need to create a separate client to check for EndpointSlice access in k8s cluster
	// when slices are enabled and registered, k8sAPI is initialized with 'ES' resource
	k8Client, err := pkgK8s.NewAPI(*kubeConfigPath, "", "", []string{}, 0)
	if err != nil {
		log.Fatalf("Failed to initialize K8s API Client: %s", err)
	}

	ctx := context.Background()

	err = pkgK8s.EndpointSliceAccess(ctx, k8Client)
	if *enableEndpointSlices && err != nil {
		log.Fatalf("Failed to start with EndpointSlices enabled: %s", err)
	}

	var k8sAPI *k8s.API
	if *enableEndpointSlices {
		k8sAPI, err = k8s.InitializeAPI(
			ctx,
			*kubeConfigPath,
			true,
			"local",
			k8s.Endpoint, k8s.ES, k8s.Pod, k8s.Svc, k8s.SP, k8s.Job, k8s.Srv, k8s.ExtWorkload,
		)
	} else {
		k8sAPI, err = k8s.InitializeAPI(
			ctx,
			*kubeConfigPath,
			true,
			"local",
			k8s.Endpoint, k8s.Pod, k8s.Svc, k8s.SP, k8s.Job, k8s.Srv, k8s.ExtWorkload,
		)
	}
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	metadataAPI, err := k8s.InitializeMetadataAPI(*kubeConfigPath, "local", k8s.Node, k8s.RS, k8s.Job)
	if err != nil {
		log.Fatalf("Failed to initialize Kubernetes metadata API: %s", err)
	}

	clusterStore, err := watcher.NewClusterStore(k8Client, *controllerNamespace, *enableEndpointSlices)
	if err != nil {
		log.Fatalf("Failed to initialize Cluster Store: %s", err)
	}

	config := destination.Config{
		ControllerNS:           *controllerNamespace,
		IdentityTrustDomain:    *trustDomain,
		ClusterDomain:          *clusterDomain,
		DefaultOpaquePorts:     opaquePorts,
		EnableH2Upgrade:        *enableH2Upgrade,
		EnableEndpointSlices:   *enableEndpointSlices,
		ExtEndpointZoneWeights: *extEndpointZoneWeights,
	}
	server, err := destination.NewServer(
		*addr,
		config,
		k8sAPI,
		metadataAPI,
		clusterStore,
		done,
	)

	if err != nil {
		log.Fatalf("Failed to initialize destination server: %s", err)
	}

	// blocks until caches are synced
	k8sAPI.Sync(nil)
	metadataAPI.Sync(nil)
	clusterStore.Sync(nil)

	// Start mesh expansion external workload controller to write endpointslices
	// to API Server.
	if *enableEndpointSlices {
		hostname, ok := os.LookupEnv("HOSTNAME")
		if !ok {
			log.Fatal("Failed to initialize External Workload Endpoints Controller, \"HOSTNAME\" value not found")
		}
		externalWorkloadController, err := externalworkload.NewEndpointsController(k8sAPI, hostname, *controllerNamespace, done, *exportControllerQueueMetrics)
		if err != nil {
			log.Fatalf("Failed to initialize External Workload Endpoints Controller: %v", err)
		}

		externalWorkloadController.Start()
	}

	go func() {
		log.Infof("starting gRPC server on %s", *addr)
		if err := server.Serve(lis); err != nil {
			log.Errorf("failed to start destination gRPC server: %s", err)
		}
	}()

	ready = true

	<-stop

	log.Infof("shutting down gRPC server on %s", *addr)
	close(done)
	server.GracefulStop()
	adminServer.Shutdown(ctx)
}
