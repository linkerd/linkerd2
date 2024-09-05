package main

import (
	"context"
	"flag"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/trace"
	api "github.com/linkerd/linkerd2/viz/metrics-api"
	promApi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/config"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func main() {
	cmd := flag.NewFlagSet("metrics-api", flag.ExitOnError)

	addr := cmd.String("addr", ":8085", "address to serve on")
	kubeConfigPath := cmd.String("kubeconfig", "", "path to kube config")
	prometheusURL := cmd.String("prometheus-url", "", "prometheus url")
	prometheusCredentials := cmd.String("prometheus-creds-secret", "", "name of the Secret containing prometheus credentials")
	metricsAddr := cmd.String("metrics-addr", ":9995", "address to serve scrapable metrics on")
	controllerNamespace := cmd.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	vizNamespace := cmd.String("viz-namespace", "linkerd-viz", "namespace in which Linkerd-Viz is installed")
	ignoredNamespaces := cmd.String("ignore-namespaces", "kube-system", "comma separated list of namespaces to not list pods from")
	clusterDomain := cmd.String("cluster-domain", "cluster.local", "kubernetes cluster domain")
	enablePprof := cmd.Bool("enable-pprof", false, "Enable pprof endpoints on the admin server")

	traceCollector := flags.AddTraceFlags(cmd)

	flags.ConfigureAndParse(cmd, os.Args[1:])

	ready := false
	adminServer := admin.NewServer(*metricsAddr, *enablePprof, &ready)

	go func() {
		log.Infof("starting admin server on %s", *metricsAddr)
		if err := adminServer.ListenAndServe(); err != nil {
			log.Errorf("failed to start metrics API admin server: %s", err)
		}
	}()

	ctx := context.Background()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	k8sAPI, err := k8s.InitializeAPI(
		ctx,
		*kubeConfigPath,
		true,
		"local",
		k8s.CJ, k8s.DS, k8s.Deploy, k8s.Job, k8s.NS, k8s.Pod, k8s.RC, k8s.RS, k8s.Svc, k8s.SS, k8s.SP,
	)
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	var prometheusClient promApi.Client
	if *prometheusURL != "" {
		promConfig := promApi.Config{Address: *prometheusURL}
		if *prometheusCredentials != "" {
			secret, err := k8sAPI.Client.CoreV1().Secrets(*vizNamespace).Get(ctx, *prometheusCredentials, metav1.GetOptions{})
			if err != nil {
				log.Fatal(err.Error())
			}
			promConfig.RoundTripper = config.NewBasicAuthRoundTripper(
				config.NewInlineSecret(string(secret.Data["user"])),
				config.NewInlineSecret(string(secret.Data["password"])),
				promApi.DefaultRoundTripper,
			)
		}
		prometheusClient, err = promApi.NewClient(promConfig)
		if err != nil {
			log.Fatal(err.Error())
		}
	}

	log.Infof("prometheusClient: %#v", prometheusClient)
	log.Info("Using cluster domain: ", *clusterDomain)

	if *traceCollector != "" {
		if err := trace.InitializeTracing("linkerd-public-api", *traceCollector); err != nil {
			log.Warnf("failed to initialize tracing: %s", err)
		}
	}

	var promAPI promv1.API
	if prometheusClient != nil {
		promAPI = promv1.NewAPI(prometheusClient)
	}

	server := api.NewGrpcServer(
		promAPI,
		k8sAPI,
		*controllerNamespace,
		*clusterDomain,
		strings.Split(*ignoredNamespaces, ","),
	)

	k8sAPI.Sync(nil) // blocks until caches are synced

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %s", *addr, err)
	}
	go func() {
		log.Infof("starting HTTP server on %+v", *addr)

		if err := server.Serve(lis); err != nil {
			log.Errorf("failed to start metrics API HTTP server: %s", err)
		}
	}()

	ready = true

	<-stop

	log.Infof("shutting down HTTP server on %+v", *addr)
	server.GracefulStop()
	adminServer.Shutdown(ctx)
}
