package spvalidator

import (
	"context"
	"flag"
	"fmt"

	validator "github.com/linkerd/linkerd2/controller/sp-validator"
	"github.com/linkerd/linkerd2/controller/webhook"
	"github.com/linkerd/linkerd2/pkg/flags"
)

// Main executes the sp-validator subcommand
func Main(args []string) {
	cmd := flag.NewFlagSet("sp-validator", flag.ExitOnError)
	metricsAddr := cmd.String("metrics-addr", fmt.Sprintf(":%d", 9997), "address to serve scrapable metrics on")
	addr := cmd.String("addr", ":8443", "address to serve on")
	kubeconfig := cmd.String("kubeconfig", "", "path to kubeconfig")
	flags.ConfigureAndParse(cmd, args)

	webhook.Launch(
		context.Background(),
		nil,
		validator.AdmitSP,
		"linkerd-sp-validator",
		*metricsAddr,
		*addr,
		*kubeconfig,
	)
}
