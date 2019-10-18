package destination

import (
	"flag"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/linkerd/linkerd2/controller/api/destination"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/flags"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/trace"
	log "github.com/sirupsen/logrus"
)

// Main executes the destination subcommand
func Main(args []string) {
	cmd := flag.NewFlagSet("destination", flag.ExitOnError)

	addr := cmd.String("addr", ":8086", "address to serve on")
	metricsAddr := cmd.String("metrics-addr", ":9996", "address to serve scrapable metrics on")
	kubeConfigPath := cmd.String("kubeconfig", "", "path to kube config")
	enableH2Upgrade := cmd.Bool("enable-h2-upgrade", true, "Enable transparently upgraded HTTP2 connections among pods in the service mesh")
	disableIdentity := cmd.Bool("disable-identity", false, "Disable identity configuration")
	controllerNamespace := cmd.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")

	traceCollector := flags.AddTraceFlags(cmd)

	flags.ConfigureAndParse(cmd, args)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	k8sAPI, err := k8s.InitializeAPI(
		*kubeConfigPath,
		k8s.Endpoint, k8s.Pod, k8s.RS, k8s.Svc, k8s.SP, k8s.TS,
	)
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	done := make(chan struct{})

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %s", *addr, err)
	}

	global, err := config.Global(consts.MountPathGlobalConfig)

	trustDomain := ""
	if *disableIdentity {
		log.Info("Identity is disabled")
	} else {
		trustDomain = global.GetIdentityContext().GetTrustDomain()
		if err != nil || trustDomain == "" {
			trustDomain = "cluster.local"
			log.Warnf("failed to load trust domain from global config: [%s] (falling back to %s)", err, trustDomain)
		}
	}

	clusterDomain := global.GetClusterDomain()
	if err != nil || clusterDomain == "" {
		clusterDomain = "cluster.local"
		log.Warnf("failed to load cluster domain from global config: [%s] (falling back to %s)", err, clusterDomain)
	}

	if *traceCollector != "" {
		if err := trace.InitializeTracing("linkerd-destination", *traceCollector); err != nil {
			log.Warnf("failed to initialize tracing: %s", err)
		}
	}

	server := destination.NewServer(
		*addr,
		*controllerNamespace,
		trustDomain,
		*enableH2Upgrade,
		k8sAPI,
		clusterDomain,
		done,
	)

	k8sAPI.Sync() // blocks until caches are synced

	go func() {
		log.Infof("starting gRPC server on %s", *addr)
		server.Serve(lis)
	}()

	go admin.StartServer(*metricsAddr)

	<-stop

	log.Infof("shutting down gRPC server on %s", *addr)
	close(done)
	server.GracefulStop()
}
