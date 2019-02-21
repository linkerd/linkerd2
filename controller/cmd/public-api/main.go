package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/linkerd/linkerd2/controller/api/discovery"
	"github.com/linkerd/linkerd2/controller/api/public"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/controller/tap"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	promApi "github.com/prometheus/client_golang/api"
	log "github.com/sirupsen/logrus"
)

func main() {
	addr := flag.String("addr", ":8085", "address to serve on")
	kubeConfigPath := flag.String("kubeconfig", "", "path to kube config")
	prometheusURL := flag.String("prometheus-url", "http://127.0.0.1:9090", "prometheus url")
	metricsAddr := flag.String("metrics-addr", ":9995", "address to serve scrapable metrics on")
	destinationAPIAddr := flag.String("destination-addr", "127.0.0.1:8086", "address of destination service")
	tapAddr := flag.String("tap-addr", "127.0.0.1:8088", "address of tap service")
	controllerNamespace := flag.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	flags.ConfigureAndParse()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	tapClient, tapConn, err := tap.NewClient(*tapAddr)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer tapConn.Close()

	discoveryClient, discoveryConn, err := discovery.NewClient(*destinationAPIAddr)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer discoveryConn.Close()

	k8sAPI, err := k8s.InitializeAPI(
		*kubeConfigPath, *controllerNamespace,
		k8s.DS, k8s.Deploy, k8s.Pod, k8s.RC, k8s.RS, k8s.Svc, k8s.SS, k8s.SP,
	)
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	prometheusClient, err := promApi.NewClient(promApi.Config{Address: *prometheusURL})
	if err != nil {
		log.Fatal(err.Error())
	}

	server := public.NewServer(
		*addr,
		prometheusClient,
		tapClient,
		discoveryClient,
		k8sAPI,
		*controllerNamespace,
	)

	k8sAPI.Sync() // blocks until caches are synced

	go func() {
		log.Infof("starting HTTP server on %+v", *addr)
		server.ListenAndServe()
	}()

	go admin.StartServer(*metricsAddr)

	<-stop

	log.Infof("shutting down HTTP server on %+v", *addr)
	server.Shutdown(context.Background())
}
