package tapinjector

import (
	"context"
	"flag"
	"fmt"

	"github.com/linkerd/linkerd2/controller/k8s"
	tapinjector "github.com/linkerd/linkerd2/controller/tap-injector"
	"github.com/linkerd/linkerd2/controller/webhook"
	"github.com/linkerd/linkerd2/pkg/flags"
)

// Main executes the tap-injector subcommand
func Main(args []string) {
	cmd := flag.NewFlagSet("tap-injector", flag.ExitOnError)
	metricsAddr := cmd.String("metrics-addr", fmt.Sprintf(":%d", 9995), "address to serve scrapable metrics on")
	addr := cmd.String("addr", ":8443", "address to serve on")
	kubeconfig := cmd.String("kubeconfig", "", "path to kubeconfig")
	tapSvcName := cmd.String("tap-service-name", "", "service name of the tap service")
	flags.ConfigureAndParse(cmd, args)
	webhook.Launch(
		context.Background(),
		[]k8s.APIResource{k8s.NS},
		tapinjector.Mutate(*tapSvcName),
		"tap-injector",
		*metricsAddr,
		*addr,
		*kubeconfig,
	)
}
