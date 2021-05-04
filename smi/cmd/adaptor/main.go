package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	spclientset "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	spinformers "github.com/linkerd/linkerd2/controller/gen/client/informers/externalversions"
	k8sAPI "github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/trace"
	"github.com/linkerd/linkerd2/smi/adaptor"
	tsclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	tsinformers "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/informers/externalversions"
	log "github.com/sirupsen/logrus"
)

func main() {
	cmd := flag.NewFlagSet("smi-adaptor", flag.ExitOnError)

	kubeConfigPath := cmd.String("kubeconfig", "", "path to kube config")
	metricsAddr := cmd.String("metrics-addr", ":9995", "address to serve scrapable metrics on")
	clusterDomain := cmd.String("cluster-domain", "cluster.local", "kubernetes cluster domain")

	traceCollector := flags.AddTraceFlags(cmd)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	done := make(chan struct{}, 1)

	go func() {
		// Close done when stop signal is received
		<-stop
		close(done)
	}()

	ctx := context.Background()
	config, err := k8s.GetConfig(*kubeConfigPath, "")
	if err != nil {
		log.Fatalf("error configuring Kubernetes API client: %v", err)
	}

	k8sAPI, err := k8sAPI.InitializeAPI(
		ctx,
		*kubeConfigPath,
		false,
		k8sAPI.SP, k8sAPI.TS,
	)
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	log.Info("Using cluster domain: ", *clusterDomain)

	if *traceCollector != "" {
		if err := trace.InitializeTracing("linkerd-public-api", *traceCollector); err != nil {
			log.Warnf("failed to initialize tracing: %s", err)
		}
	}

	spClient, err := spclientset.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error building example clientset: %s", err.Error())
	}

	tsClient, err := tsclientset.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error building example clientset: %s", err.Error())
	}

	spInformerFactory := spinformers.NewSharedInformerFactory(spClient, time.Second*30)
	tsInformerFactory := tsinformers.NewSharedInformerFactory(tsClient, time.Second*30)

	controller := adaptor.NewController(
		k8sAPI.Client,
		*clusterDomain,
		tsClient,
		spClient,
		spInformerFactory.Linkerd().V1alpha2().ServiceProfiles(),
		tsInformerFactory.Split().V1alpha1().TrafficSplits(),
	)

	spInformerFactory.Start(done)
	tsInformerFactory.Start(done)

	if err = controller.Run(done); err != nil {
		log.Fatalf("Error running controller: %s", err.Error())
	}
	go admin.StartServer(*metricsAddr)

	log.Info("Shutting down")
}
