package servicemirror

import (
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/flags"
	log "github.com/sirupsen/logrus"
)

const (
	Prefix                 = "svcmirror.io"
	MirrorSecretType       = Prefix + "/remote-kubeconfig"
	GatewayNameAnnotation  = Prefix + "/gateway-name"
	RemoteGatewayNameAnnotation  = Prefix + "/remote-gateway-name"

	GatewayNsAnnottion     = Prefix + "/gateway-ns"
	RemoteGatewayNsAnnottion     = Prefix + "/remote-gateway-ns"
	MirroredResourceLabel  = Prefix + "/mirrored-service"
	RemoteClusterNameLabel = Prefix + "/cluster-name"


	RemoteResourceVersionLabel = Prefix + "/remote-resource-version"



	ConfigKeyName          = "kubeconfig"
)

func Main(args []string) {
	cmd := flag.NewFlagSet("service-mirror", flag.ExitOnError)

	kubeConfigPath := cmd.String("kubeconfig", "", "path to kube config")

	flags.ConfigureAndParse(cmd, args)
	log.SetLevel(log.DebugLevel)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	k8sAPI, err := k8s.InitializeAPI(
		*kubeConfigPath,
		k8s.SC,
		k8s.Svc,
		k8s.NS,
		k8s.Endpoint,
	)

	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	k8sAPI.Sync()
	_ = NewRemoteClusterConfigWatcher(k8sAPI)
	time.Sleep(100 * time.Hour) // wait forever... for now :)
}
