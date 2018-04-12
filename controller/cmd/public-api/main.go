package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	promApi "github.com/prometheus/client_golang/api"
	"github.com/runconduit/conduit/controller/api/public"
	"github.com/runconduit/conduit/controller/k8s"
	"github.com/runconduit/conduit/controller/tap"
	"github.com/runconduit/conduit/controller/util"
	"github.com/runconduit/conduit/pkg/version"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

func main() {
	addr := flag.String("addr", ":8085", "address to serve on")
	kubeConfigPath := flag.String("kubeconfig", "", "path to kube config")
	prometheusUrl := flag.String("prometheus-url", "http://127.0.0.1:9090", "prometheus url")
	metricsAddr := flag.String("metrics-addr", ":9995", "address to serve scrapable metrics on")
	tapAddr := flag.String("tap-addr", "127.0.0.1:8088", "address of tap service")
	controllerNamespace := flag.String("controller-namespace", "conduit", "namespace in which Conduit is installed")
	ignoredNamespaces := flag.String("ignore-namespaces", "kube-system", "comma separated list of namespaces to not list pods from")
	logLevel := flag.String("log-level", log.InfoLevel.String(), "log level, must be one of: panic, fatal, error, warn, info, debug")
	printVersion := version.VersionFlag()
	flag.Parse()

	// set global log level
	level, err := log.ParseLevel(*logLevel)
	if err != nil {
		log.Fatalf("invalid log-level: %s", *logLevel)
	}
	log.SetLevel(level)

	version.MaybePrintVersionAndExit(*printVersion)

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

	sharedInformers := informers.NewSharedInformerFactory(k8sClient, 10*time.Minute)

	namespaceInformer := sharedInformers.Core().V1().Namespaces()
	namespaceInformerSynced := namespaceInformer.Informer().HasSynced

	deployInformer := sharedInformers.Apps().V1beta2().Deployments()
	deployInformerSynced := deployInformer.Informer().HasSynced

	replicaSetInformer := sharedInformers.Apps().V1beta2().ReplicaSets()
	replicaSetInformerSynced := replicaSetInformer.Informer().HasSynced

	podInformer := sharedInformers.Core().V1().Pods()
	podInformerSynced := podInformer.Informer().HasSynced

	sharedInformers.Start(nil)

	prometheusClient, err := promApi.NewClient(promApi.Config{Address: *prometheusUrl})
	if err != nil {
		log.Fatal(err.Error())
	}

	server := public.NewServer(
		*addr,
		prometheusClient,
		tapClient,
		namespaceInformer.Lister(),
		deployInformer.Lister(),
		replicaSetInformer.Lister(),
		podInformer.Lister(),
		*controllerNamespace,
		strings.Split(*ignoredNamespaces, ","),
	)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		log.Infof("waiting for caches to sync")
		if !cache.WaitForCacheSync(
			ctx.Done(),
			namespaceInformerSynced,
			deployInformerSynced,
			replicaSetInformerSynced,
			podInformerSynced,
		) {
			log.Fatalf("timed out wait for caches to sync")
		}
		log.Infof("caches synced")
	}()

	go func() {
		log.Infof("starting HTTP server on %+v", *addr)
		server.ListenAndServe()
	}()

	go util.NewMetricsServer(*metricsAddr)

	<-stop

	log.Infof("shutting down HTTP server on %+v", *addr)
	server.Shutdown(context.Background())
}
