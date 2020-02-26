package servicemirror

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/flags"
	log "github.com/sirupsen/logrus"
)

// Main executes the tap service-mirror
func Main(args []string) {
	cmd := flag.NewFlagSet("service-mirror", flag.ExitOnError)

	kubeConfigPath := cmd.String("kubeconfig", "", "path to the local kube config")

	flags.ConfigureAndParse(cmd, args)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	k8sAPI, err := k8s.InitializeAPI(
		*kubeConfigPath,
		false,
		k8s.Secret,
		k8s.Svc,
		k8s.NS,
		k8s.Endpoint,
	)

	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	k8sAPI.Sync(nil)
	watcher := NewRemoteClusterConfigWatcher(k8sAPI)
	log.Info("Started cluster config watcher")

	<-stop

	log.Info("Stopping cluster config watcher")
	watcher.Stop()
}
