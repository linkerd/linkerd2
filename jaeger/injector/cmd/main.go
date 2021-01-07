package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/controller/webhook"
	"github.com/linkerd/linkerd2/jaeger/injector/mutator"
	"github.com/linkerd/linkerd2/pkg/flags"
)

func main() {
	cmd := flag.NewFlagSet("injector", flag.ExitOnError)
	metricsAddr := cmd.String("metrics-addr", fmt.Sprintf(":%d", 9995),
		"address to serve scrapable metrics on")
	addr := cmd.String("addr", ":8443", "address to serve on")
	kubeconfig := cmd.String("kubeconfig", "", "path to kubeconfig")
	collectorSvcAddr := cmd.String("collector-svc-addr", "",
		"collector service address for the proxies to send trace data")
	collectorSvcAccount := cmd.String("collector-svc-account", "",
		"service account associated with the collector instance")

	flags.ConfigureAndParse(cmd, os.Args[1:])

	webhook.Launch(
		context.Background(),
		[]k8s.APIResource{k8s.NS},
		mutator.Mutate(*collectorSvcAddr, *collectorSvcAccount),
		"linkerd-jaeger-injector",
		*metricsAddr,
		*addr,
		*kubeconfig,
	)
}
