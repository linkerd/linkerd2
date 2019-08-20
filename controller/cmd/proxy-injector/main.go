package main

import (
	"github.com/linkerd/linkerd2/controller/k8s"
	injector "github.com/linkerd/linkerd2/controller/proxy-injector"
	"github.com/linkerd/linkerd2/controller/webhook"
)

func main() {
	webhook.Launch(
		[]k8s.APIResource{k8s.NS, k8s.Deploy, k8s.RC, k8s.RS, k8s.Job, k8s.DS, k8s.SS, k8s.Pod},
		9995,
		injector.Inject,
		"linkerd-proxy-injector",
	)
}
