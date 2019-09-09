package spvalidator

import (
	validator "github.com/linkerd/linkerd2/controller/sp-validator"
	"github.com/linkerd/linkerd2/controller/webhook"
)

// Main executes the sp-validator subcommand
func Main(args []string) {
	webhook.Launch(
		nil,
		9997,
		validator.AdmitSP,
		"linkerd-sp-validator",
		"sp-validator",
		args,
	)
}
