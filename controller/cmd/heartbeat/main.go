package heartbeat

import (
	"context"
	"flag"
	"net/url"

	"github.com/linkerd/linkerd2/controller/heartbeat"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	promApi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	log "github.com/sirupsen/logrus"
)

// Main executes the heartbeat subcommand
func Main(args []string) {
	cmd := flag.NewFlagSet("heartbeat", flag.ExitOnError)

	kubeConfigPath := cmd.String("kubeconfig", "", "path to kube config")
	prometheusURL := cmd.String("prometheus-url", "http://127.0.0.1:9090", "prometheus url")
	controllerNamespace := cmd.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")

	flags.ConfigureAndParse(cmd, args)

	// Gather the following fields:
	// - version
	// - source
	// - uuid
	// - k8s-version
	// - install-time
	// - rps
	// - meshed-pods
	// - proxy-injector-injections
	// TODO:
	// - k8s-env
	v := url.Values{}
	v.Set("version", version.Version)
	v.Set("source", "heartbeat")

	kubeAPI, err := k8s.NewAPI(*kubeConfigPath, "", "", []string{}, 0)
	if err != nil {
		log.Errorf("Failed to initialize k8s API: %s", err)
	} else {
		k8sV := heartbeat.K8sValues(context.Background(), kubeAPI, *controllerNamespace)
		v = heartbeat.MergeValues(v, k8sV)
	}

	prometheusClient, err := promApi.NewClient(promApi.Config{Address: *prometheusURL})
	if err != nil {
		log.Errorf("Failed to initialize Prometheus client: %s", err)
	} else {
		promAPI := promv1.NewAPI(prometheusClient)
		promV := heartbeat.PromValues(promAPI, *controllerNamespace)
		v = heartbeat.MergeValues(v, promV)
	}

	err = heartbeat.Send(v)
	if err != nil {
		log.Fatalf("Failed to send heartbeat: %s", err)
	}
}
