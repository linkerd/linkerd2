package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"

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
	tapAddr := flag.String("tap-addr", "127.0.0.1:8088", "address of tap service")
	controllerNamespace := flag.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	singleNamespace := flag.Bool("single-namespace", false, "only operate in the controller namespace")
	ignoredNamespaces := flag.String("ignore-namespaces", "kube-system", "comma separated list of namespaces to not list pods from")
	flags.ConfigureAndParse()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	tapClient, tapConn, err := tap.NewClient(*tapAddr)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer tapConn.Close()

	k8sClient, err := k8s.NewClientSet(*kubeConfigPath)
	if err != nil {
		log.Fatal(err.Error())
	}
	restrictToNamespace := ""
	if *singleNamespace {
		restrictToNamespace = *controllerNamespace
	}
	k8sAPI := k8s.NewAPI(
		k8sClient,
		nil,
		restrictToNamespace,
		k8s.Deploy,
		k8s.Pod,
		k8s.RC,
		k8s.RS,
		k8s.Svc,
	)

	prometheusClient, err := promApi.NewClient(promApi.Config{Address: *prometheusURL})
	if err != nil {
		log.Fatal(err.Error())
	}

	server := public.NewServer(
		*addr,
		prometheusClient,
		tapClient,
		k8sAPI,
		*controllerNamespace,
		strings.Split(*ignoredNamespaces, ","),
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
