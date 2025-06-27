package proxyinjector

import (
	"context"
	"flag"
	"fmt"

	"github.com/linkerd/linkerd2/controller/k8s"
	injector "github.com/linkerd/linkerd2/controller/proxy-injector"
	"github.com/linkerd/linkerd2/controller/webhook"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/inject"
)

// Main executes the proxy-injector subcommand
func Main(args []string) {
	cmd := flag.NewFlagSet("proxy-injector", flag.ExitOnError)
	metricsAddr := cmd.String("metrics-addr", fmt.Sprintf(":%d", 9995), "address to serve scrapable metrics on")
	addr := cmd.String("addr", ":8443", "address to serve on")
	kubeconfig := cmd.String("kubeconfig", "", "path to kubeconfig")
	linkerdNamespace := cmd.String("linkerd-namespace", "linkerd", "control plane namespace")
	enablePprof := cmd.Bool("enable-pprof", false, "Enable pprof endpoints on the admin server")
	flags.ConfigureAndParse(cmd, args)

	webhook.Launch(
		context.Background(),
		[]k8s.APIResource{k8s.NS, k8s.Deploy, k8s.RC, k8s.RS, k8s.Job, k8s.DS, k8s.SS, k8s.Pod, k8s.CJ},
		injector.Inject(*linkerdNamespace, inject.GetOverriddenValues, inject.PatchProducers),
		"linkerd-proxy-injector",
		*metricsAddr,
		*addr,
		*kubeconfig,
		*enablePprof,
	)
}
