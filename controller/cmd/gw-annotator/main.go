package gwannotatorcmd

import (
	gwannotator "github.com/linkerd/linkerd2/controller/gw-annotator"
	"github.com/linkerd/linkerd2/controller/webhook"
)

// Main executes the gateway-annotator subcommand
func Main(args []string) {
	webhook.Launch(
		nil,
		9991,
		gwannotator.AnnotateGateway,
		"linkerd-gateway-annotator",
		"gateway-annotator",
		args,
	)
}
