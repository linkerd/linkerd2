package servicemirror

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/multicluster"
	log "github.com/sirupsen/logrus"
)

// Main executes the service-mirror controller
func Main(args []string) {
	cmd := flag.NewFlagSet("service-mirror", flag.ExitOnError)

	kubeConfigPath := cmd.String("kubeconfig", "", "path to the local kube config")
	_ = cmd.Int("event-requeue-limit", 3, "requeue limit for events")
	metricsAddr := cmd.String("metrics-addr", ":9999", "address to serve scrapable metrics on")
	namespace := cmd.String("namespace", "", "namespace containing Link and credentials Secret")
	//repairPeriod := cmd.Duration("endpoint-refresh-period", 1*time.Minute, "frequency to refresh endpoint resolution")

	flags.ConfigureAndParse(cmd, args)
	linkName := cmd.Arg(0)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	k8sAPI, err := k8s.NewAPI(*kubeConfigPath, "", "", []string{}, 0)

	//TODO: Use can-i to check for required permissions
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	gvr := schema.GroupVersionResource{
		Group:    "multicluster.linkerd.io",
		Version:  "v1alpha1",
		Resource: "links",
	}
	unstructured, err := k8sAPI.DynamicClient.Resource(gvr).Namespace(*namespace).Get(linkName, metav1.GetOptions{})
	if err != nil {
		log.Fatalf("Failed to fetch Link %s: %s", linkName, err)
	}
	link, err := multicluster.NewLink(*unstructured)

	if err != nil {
		log.Fatalf("Failed to parse Link %s: %s", linkName, err)
	}

	log.Infof("Loaded Link %s: %+v", linkName, link)

	go admin.StartServer(*metricsAddr)

	<-stop
	log.Info("Stopping service mirror controller")
}
