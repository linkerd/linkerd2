package proxyinjector

import (
	"github.com/linkerd/linkerd2/controller/k8s"
	injector "github.com/linkerd/linkerd2/controller/proxy-injector"
	"github.com/linkerd/linkerd2/controller/webhook"
)

// Main executes the proxy-injector subcommand
func Main(args []string) {
	webhook.Launch(
		[]k8s.APIResource{k8s.RT(k8s.NS), k8s.RT(k8s.Deploy), k8s.RT(k8s.RC), k8s.RT(k8s.RS), k8s.RT(k8s.Job), k8s.RT(k8s.DS), k8s.RT(k8s.SS), k8s.RT(k8s.Pod), k8s.RT(k8s.CJ)},
		9995,
		injector.Inject,
		"linkerd-proxy-injector",
		"proxy-injector",
		args,
	)
}
