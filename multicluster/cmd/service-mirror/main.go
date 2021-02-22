package servicemirror

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	controllerK8s "github.com/linkerd/linkerd2/controller/k8s"
	servicemirror "github.com/linkerd/linkerd2/multicluster/service-mirror"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/multicluster"
	sm "github.com/linkerd/linkerd2/pkg/servicemirror"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	dynamic "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/clientcmd"
)

const linkWatchRestartAfter = 10 * time.Second

var (
	clusterWatcher *servicemirror.RemoteClusterServiceWatcher
	probeWorker    *servicemirror.ProbeWorker
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
	// k8sAPI is used as a dynamic client for unstructured access to Link custom
	// resources.
	//
	// controllerK8sAPI is used by the cluster watcher to manage
	// mirror resources such as services, namespaces, and endpoints.
	k8sAPI, err := k8s.NewAPI(*kubeConfigPath, "", "", []string{}, 0)
	//TODO: Use can-i to check for required permissions
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	ctx := context.Background()
	controllerK8sAPI, err := controllerK8s.InitializeAPI(
		ctx,
		*kubeConfigPath,
		false,
		controllerK8s.NS,
		controllerK8s.Svc,
		controllerK8s.Endpoint,
	)
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	linkClient := k8sAPI.DynamicClient.Resource(multicluster.LinkGVR).Namespace(*namespace)

	metrics := servicemirror.NewProbeMetricVecs()
	go admin.StartServer(*metricsAddr)

	controllerK8sAPI.Sync(nil)

main:
	for {
		// Start link watch
		linkWatch, err := linkClient.Watch(ctx, metav1.ListOptions{})
		if err != nil {
			log.Fatalf("Failed to watch Link %s: %s", linkName, err)
		}
		results := linkWatch.ResultChan()

		// Each time the link resource is updated, reload the config and restart the
		// cluster watcher.
		for {
			select {
			case <-stop:
				break main
			case event, ok := <-results:
				if !ok {
					log.Info("Link watch terminated; restarting watch")
					continue main
				}
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
							creds, err := loadCredentials(ctx, link, *namespace, k8sAPI)
							if err != nil {
								log.Errorf("Failed to load remote cluster credentials: %s", err)
							}
							err = restartClusterWatcher(ctx, link, *namespace, creds, controllerK8sAPI, *requeueLimit, *repairPeriod, metrics)
							if err != nil {
								// failed to restart cluster watcher; give a bit of slack
								// and restart the link watch to give it another try
								log.Error(err)
								time.Sleep(linkWatchRestartAfter)
								linkWatch.Stop()
							}
						case watch.Deleted:
							log.Infof("Link %s deleted", linkName)
							if clusterWatcher != nil {
								clusterWatcher.Stop(false)
								clusterWatcher = nil
							}
							if probeWorker != nil {
								probeWorker.Stop()
								probeWorker = nil
							}
						default:
							log.Infof("Ignoring event type %s", event.Type)
						}
					}
				default:
					log.Errorf("Unknown object type detected: %+v", obj)
				}
			}
		}
	}
	log.Info("Shutting down")
}

func loadCredentials(ctx context.Context, link multicluster.Link, namespace string, k8sAPI *k8s.KubernetesAPI) ([]byte, error) {
	// Load the credentials secret
	secret, err := k8sAPI.Interface.CoreV1().Secrets(namespace).Get(ctx, link.ClusterCredentialsSecret, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("Failed to load credentials secret %s: %s", link.ClusterCredentialsSecret, err)
	}
	return sm.ParseRemoteClusterSecret(secret)
}

func restartClusterWatcher(
	ctx context.Context,
	link multicluster.Link,
	namespace string,
	creds []byte,
	controllerK8sAPI *controllerK8s.API,
	requeueLimit int,
	repairPeriod time.Duration,
	metrics servicemirror.ProbeMetricVecs,
) error {
	if clusterWatcher != nil {
		clusterWatcher.Stop(false)
	}
	if probeWorker != nil {
		probeWorker.Stop()
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig(creds)
	if err != nil {
		return fmt.Errorf("Unable to parse kube config: %s", err)
	}

	clusterWatcher, err = servicemirror.NewRemoteClusterServiceWatcher(
		ctx,
		namespace,
		controllerK8sAPI,
		cfg,
		&link,
		requeueLimit,
		repairPeriod,
	)
	if err != nil {
		return fmt.Errorf("Unable to create cluster watcher: %s", err)
	}

	err = clusterWatcher.Start(ctx)
	if err != nil {
		return fmt.Errorf("Failed to start cluster watcher: %s", err)
	}

	workerMetrics, err := metrics.NewWorkerMetrics(link.TargetClusterName)
	if err != nil {
		return fmt.Errorf("Failed to create metrics for cluster watcher: %s", err)
	}
	probeWorker = servicemirror.NewProbeWorker(fmt.Sprintf("probe-gateway-%s", link.TargetClusterName), &link.ProbeSpec, workerMetrics, link.TargetClusterName)
	probeWorker.Start()
	return nil
}
