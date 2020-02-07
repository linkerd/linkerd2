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

const (
	prefix = "svcmirror.io"

	// MirrorSecretType is the type of secret that is supposed to contain
	// the access information for remote clusters.
	MirrorSecretType = prefix + "/remote-kubeconfig"

	// GatewayNameAnnotation is the annotation that is present on the remote
	// service, indicating which gateway is supposed to route traffic to it
	GatewayNameAnnotation = prefix + "/gateway-name"

	// RemoteGatewayNameLabel is same as GatewayNameAnnotation but on the local,
	// mirrored service. It's used for quick querying when we want to figure out
	// the services that are being associated with a certain gateway
	RemoteGatewayNameLabel = prefix + "/remote-gateway-name"

	// GatewayNsAnnotation is present on the remote service, indicating the ns
	// in which we can find the gateway
	GatewayNsAnnotation = prefix + "/gateway-ns"

	// RemoteGatewayNsLabel follows the same kind of logic as RemoteGatewayNameLabel
	RemoteGatewayNsLabel = prefix + "/remote-gateway-ns"

	// MirroredResourceLabel indicates that this resource is the result
	// of a mirroring operation (can be a namespace or a service)
	MirroredResourceLabel = prefix + "/mirrored-service"

	// RemoteClusterNameLabel put on a local mirrored service, it
	// allows us to associate a mirrored service with a remote cluster
	RemoteClusterNameLabel = prefix + "/cluster-name"

	// RemoteResourceVersionLabel is the last observed remote resource
	// version of a mirrored resource. Useful when doing updates
	RemoteResourceVersionLabel = prefix + "/remote-resource-version"

	// RemoteGatewayResourceVersionLabel is the last observed remote resource
	// version of the gateway for a particular mirrored service. It is used
	// in cases we detect a change in a remote gateway
	RemoteGatewayResourceVersionLabel = prefix + "/remote-gateway-resource-version"

	// ConfigKeyName is the key in the secret that stores the kubeconfig needed to connect
	// to a remote cluster
	ConfigKeyName = "kubeconfig"
)

// Main executes the tap service-mirror
func Main(args []string) {
	cmd := flag.NewFlagSet("service-mirror", flag.ExitOnError)

	kubeConfigPath := cmd.String("kubeconfig", "", "path to kube config")
	requeueLimit := cmd.Int("event-requeue-limit", 3, "requeue limit for events")

	flags.ConfigureAndParse(cmd, args)

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
	watcher := NewRemoteClusterConfigWatcher(k8sAPI, *requeueLimit)
	log.Info("Started cluster config watcher")

	<-stop

	log.Info("Stopping cluster config watcher")
	watcher.Stop()
}
