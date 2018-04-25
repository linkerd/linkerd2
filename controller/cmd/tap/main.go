package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/runconduit/conduit/controller/k8s"
	"github.com/runconduit/conduit/controller/tap"
	"github.com/runconduit/conduit/controller/util"
	"github.com/runconduit/conduit/pkg/version"
	log "github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8088", "address to serve on")
	metricsAddr := flag.String("metrics-addr", ":9998", "address to serve scrapable metrics on")
	kubeConfigPath := flag.String("kubeconfig", "", "path to kube config")
	tapPort := flag.Uint("tap-port", 4190, "proxy tap port to connect to")
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

	clientSet, err := k8s.NewClientSet(*kubeConfigPath)
	if err != nil {
		log.Fatalf("failed to create Kubernetes client: %s", err)
	}

	replicaSets, err := k8s.NewReplicaSetStore(clientSet)
	if err != nil {
		log.Fatalf("NewReplicaSetStore failed: %s", err)
	}
	err = replicaSets.Run()
	if err != nil {
		log.Fatalf("replicaSets.Run() failed: %s", err)
	}

	// index pods by deployment
	deploymentIndex := func(obj interface{}) ([]string, error) {
		pod, ok := obj.(*v1.Pod)
		if !ok {
			return nil, fmt.Errorf("object is not a Pod")
		}
		deployment, err := replicaSets.GetDeploymentForPod(pod)
		if err != nil {
			log.Debugf("Cannot get deployment for pod %s: %s", pod.Name, err)
			return []string{}, nil
		}
		return []string{deployment}, nil
	}

	pods, err := k8s.NewPodIndex(clientSet, deploymentIndex)
	if err != nil {
		log.Fatalf("NewPodIndex failed: %s", err)
	}
	err = pods.Run()
	if err != nil {
		log.Fatalf("pods.Run() failed: %s", err)
	}

	lister := k8s.NewLister(clientSet)

	server, lis, err := tap.NewServer(*addr, *tapPort, replicaSets, pods, lister)
	if err != nil {
		log.Fatal(err.Error())
	}

	go func() {
		err := lister.Sync()
		if err != nil {
			log.Fatalf("timed out wait for caches to sync: %s", err)
		}
	}()

	go func() {
		log.Println("starting gRPC server on", *addr)
		server.Serve(lis)
	}()

	go util.NewMetricsServer(*metricsAddr)

	<-stop

	log.Println("shutting down gRPC server on", *addr)
	server.GracefulStop()
}
