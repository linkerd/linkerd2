package servicemirror

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	dynamic "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/clientcmd"

	controllerK8s "github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/multicluster"
	"github.com/linkerd/linkerd2/pkg/servicemirror"
	log "github.com/sirupsen/logrus"
)

var (
	clusterWatcher *RemoteClusterServiceWatcher
	probeWorker    *ProbeWorker
)

// Main executes the service-mirror controller
func Main(args []string) {
	cmd := flag.NewFlagSet("service-mirror", flag.ExitOnError)

	kubeConfigPath := cmd.String("kubeconfig", "", "path to the local kube config")
	requeueLimit := cmd.Int("event-requeue-limit", 3, "requeue limit for events")
	metricsAddr := cmd.String("metrics-addr", ":9999", "address to serve scrapable metrics on")
	namespace := cmd.String("namespace", "", "namespace containing Link and credentials Secret")
	repairPeriod := cmd.Duration("endpoint-refresh-period", 1*time.Minute, "frequency to refresh endpoint resolution")

	flags.ConfigureAndParse(cmd, args)
	linkName := cmd.Arg(0)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// We create two different kubernetes API clients for the local cluster:
	// k8sAPI is used as a dynamic client for unstrcutured access to Link custom
	// resources.
	//
	// controllerK8sAPI is used by the cluster watcher to manage
	// mirror resources such as services, namespaces, and endpoints.
	k8sAPI, err := k8s.NewAPI(*kubeConfigPath, "", "", []string{}, 0)
	//TODO: Use can-i to check for required permissions
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	controllerK8sAPI, err := controllerK8s.InitializeAPI(*kubeConfigPath, false,
		controllerK8s.NS,
		controllerK8s.Svc,
		controllerK8s.Endpoint,
	)
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	linkClient := k8sAPI.DynamicClient.Resource(multicluster.LinkGVR).Namespace(*namespace)

	metrics := newProbeMetricVecs()
	go admin.StartServer(*metricsAddr)

	controllerK8sAPI.Sync(nil)

	for {
		// Start link watch
		linkWatch, err := linkClient.Watch(metav1.ListOptions{})
		if err != nil {
			log.Fatalf("Failed to watch Link %s: %s", linkName, err)
		}
		results := linkWatch.ResultChan()

		// Each time the link resource is updated, reload the config and restart the
		// cluster watcher.
		for event := range results {
			switch obj := event.Object.(type) {
			case *dynamic.Unstructured:
				if obj.GetName() == linkName {
					switch event.Type {
					case watch.Added, watch.Modified:
						link, err := multicluster.NewLink(*obj)
						if err != nil {
							log.Errorf("Failed to parse link %s: %s", linkName, err)
							continue
						}
						log.Infof("Got updated link %s: %+v", linkName, link)
						creds, err := loadCredentials(link, *namespace, k8sAPI)
						if err != nil {
							log.Errorf("Failed to load remote cluster credentials: %s", err)
						}
						restartClusterWatcher(link, *namespace, creds, controllerK8sAPI, *requeueLimit, *repairPeriod, metrics)
					case watch.Deleted:
						log.Infof("Link %s deleted", linkName)
						// TODO: should we delete all mirror resources?
					default:
						log.Infof("Ignoring event type %s", event.Type)
					}
				}
			default:
				log.Errorf("Unknown object type detected: %+v", obj)
			}
		}

		log.Info("Link watch terminated; restarting watch")
	}
}

func loadCredentials(link multicluster.Link, namespace string, k8sAPI *k8s.KubernetesAPI) ([]byte, error) {
	// Load the credentials secret
	secret, err := k8sAPI.Interface.CoreV1().Secrets(namespace).Get(link.ClusterCredentialsSecret, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("Failed to load credentials secret %s: %s", link.ClusterCredentialsSecret, err)
	}
	return servicemirror.ParseRemoteClusterSecret(secret)
}

func restartClusterWatcher(
	link multicluster.Link,
	namespace string,
	creds []byte,
	controllerK8sAPI *controllerK8s.API,
	requeueLimit int,
	repairPeriod time.Duration,
	metrics probeMetricVecs,
) {
	if clusterWatcher != nil {
		clusterWatcher.Stop(false)
	}
	if probeWorker != nil {
		probeWorker.Stop()
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig(creds)
	if err != nil {
		log.Errorf("Unable to parse kube config: %s", err)
		return
	}

	clusterWatcher, err = NewRemoteClusterServiceWatcher(
		namespace,
		controllerK8sAPI,
		cfg,
		&link,
		requeueLimit,
		repairPeriod,
	)
	if err != nil {
		log.Errorf("Unable to create cluster watcher: %s", err)
		return
	}

	err = clusterWatcher.Start()
	if err != nil {
		log.Errorf("Failed to start cluster watcher: %s", err)
		return
	}

	workerMetrics, err := metrics.newWorkerMetrics(link.TargetClusterName)
	if err != nil {
		log.Errorf("Failed to create metrics for cluster watcher: %s", err)
	}
	probeWorker = NewProbeWorker(fmt.Sprintf("probe-gateway-%s", link.TargetClusterName), &link.ProbeSpec, workerMetrics, link.TargetClusterName)
	go probeWorker.run()
}
