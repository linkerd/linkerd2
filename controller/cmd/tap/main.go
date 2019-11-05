package tap

import (
	"context"
	"crypto/tls"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/controller/tap"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/flags"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
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
	controllerNamespace := cmd.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	tapPort := cmd.Uint("tap-port", 4190, "proxy tap port to connect to")
	tlsCertPath := cmd.String("tls-cert", pkgK8s.MountPathTLSCrtPEM, "path to TLS Cert PEM")
	tlsKeyPath := cmd.String("tls-key", pkgK8s.MountPathTLSKeyPEM, "path to TLS Key PEM")
	disableCommonNames := cmd.Bool("disable-common-names", false, "disable checks for Common Names (for development)")

	traceCollector := flags.AddTraceFlags(cmd)

	flags.ConfigureAndParse(cmd, args)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	k8sAPI, err := k8s.InitializeAPI(
		*kubeConfigPath,
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

	globalConfig, err := config.Global(pkgK8s.MountPathGlobalConfig)
	if err != nil {
		log.Fatal(err)
	}
	trustDomain := globalConfig.GetIdentityContext().GetTrustDomain()
	if trustDomain == "" {
		trustDomain = defaultDomain
	}
	log.Infof("Using trust domain: %s", trustDomain)

	if *traceCollector != "" {
		if err := trace.InitializeTracing("linkerd-tap", *traceCollector); err != nil {
			log.Warnf("failed to initialize tracing: %s", err)
		}
	}
	grpcTapServer := tap.NewGrpcTapServer(*tapPort, *controllerNamespace, trustDomain, k8sAPI)

	// TODO: make this configurable for local development
	cert, err := tls.LoadX509KeyPair(*tlsCertPath, *tlsKeyPath)
	if err != nil {
		log.Fatal(err.Error())
	}

	apiServer, apiLis, err := tap.NewAPIServer(*apiServerAddr, cert, k8sAPI, grpcTapServer, *disableCommonNames)
	if err != nil {
		log.Fatal(err.Error())
	}

	k8sAPI.Sync() // blocks until caches are synced

	go func() {
		log.Infof("starting APIServer on %s", *apiServerAddr)
		apiServer.ServeTLS(apiLis, "", "")
	}()

	go admin.StartServer(*metricsAddr)

	<-stop

	log.Infof("shutting down APIServer on %s", *apiServerAddr)
	apiServer.Shutdown(context.Background())
}
