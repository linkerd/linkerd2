package api

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/trace"
	log "github.com/sirupsen/logrus"
)

const defaultDomain = "cluster.local"

// Main executes the tap subcommand
func Main(args []string) {
	cmd := flag.NewFlagSet("tap", flag.ExitOnError)
	apiServerAddr := cmd.String("apiserver-addr", ":8089", "address to serve the apiserver on")
	metricsAddr := cmd.String("metrics-addr", ":9998", "address to serve scrapable metrics on")
	kubeConfigPath := cmd.String("kubeconfig", "", "path to kube config")
	apiNamespace := cmd.String("api-namespace", "linkerd", "namespace in which Linkerd is installed")
	tapPort := cmd.Uint("tap-port", 4190, "proxy tap port to connect to")
	disableCommonNames := cmd.Bool("disable-common-names", false, "disable checks for Common Names (for development)")
	trustDomain := cmd.String("identity-trust-domain", defaultDomain, "configures the name suffix used for identities")
	traceCollector := flags.AddTraceFlags(cmd)
	flags.ConfigureAndParse(cmd, args)
	ctx := context.Background()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	k8sAPI, err := k8s.InitializeAPI(
		ctx,
		*kubeConfigPath,
		true,
		k8s.CJ,
		k8s.DS,
		k8s.SS,
		k8s.Deploy,
		k8s.Job,
		k8s.NS,
		k8s.Pod,
		k8s.RC,
		k8s.Svc,
		k8s.RS,
		k8s.Node,
	)
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}
	log.Infof("Using trust domain: %s", *trustDomain)
	if *traceCollector != "" {
		if err := trace.InitializeTracing("linkerd-tap", *traceCollector); err != nil {
			log.Warnf("failed to initialize tracing: %s", err)
		}
	}
	grpcTapServer := NewGrpcTapServer(*tapPort, *apiNamespace, *trustDomain, k8sAPI)
	apiServer, err := NewServer(ctx, *apiServerAddr, k8sAPI, grpcTapServer, *disableCommonNames)
	if err != nil {
		log.Fatal(err.Error())
	}
	k8sAPI.Sync(nil)
	go apiServer.Start(ctx)
	go admin.StartServer(*metricsAddr)
	<-stop
	log.Infof("shutting down APIServer on %s", *apiServerAddr)
	apiServer.Shutdown(ctx)
}
