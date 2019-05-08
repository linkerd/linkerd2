package main

import (
	validator "github.com/linkerd/linkerd2/controller/sp-validator"
	"github.com/linkerd/linkerd2/controller/webhook"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
)

func main() {
	webhook.Launch(
		nil,
		9997,
		pkgK8s.SPValidatorWebhookServiceName,
		validator.AdmitSP,
	)
}
