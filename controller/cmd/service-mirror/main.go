package servicemirror

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
)

func initLocalResourceInformer(api kubernetes.Interface, namespace string, resource k8s.APIResource) (cache.SharedIndexInformer, error) {
	sharedInformers := informers.NewSharedInformerFactoryWithOptions(api, 10*time.Minute, informers.WithNamespace(namespace))

	var informer cache.SharedIndexInformer

	switch resource {
	case k8s.Svc:
		informer = sharedInformers.Core().V1().Services().Informer()
	case k8s.Secret:
		informer = sharedInformers.Core().V1().Secrets().Informer()
	default:
		return nil, fmt.Errorf("cannot instantiate local informer for %v", resource)

	}

	sharedInformers.Start(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log.Infof("waiting for local namespaced %v informer caches to sync", resource)
	if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
		return nil, fmt.Errorf("failed to sync local namespaced %v informer caches", resource)
	}
	log.Infof("local namespaced %v informer  caches synced", resource)
	return informer, nil
}

// Main executes the tap service-mirror
func Main(args []string) {
	cmd := flag.NewFlagSet("service-mirror", flag.ExitOnError)

	kubeConfigPath := cmd.String("kubeconfig", "", "path to the local kube config")
	requeueLimit := cmd.Int("event-requeue-limit", 3, "requeue limit for events")
	metricsAddr := cmd.String("metrics-addr", ":9999", "address to serve scrapable metrics on")
	namespace := cmd.String("namespace", "", "address to serve scrapable metrics on")
	repairPeriod := cmd.Duration("endpoint-refresh-period", 1*time.Minute, "frequency to refresh endpoint resolution")

	flags.ConfigureAndParse(cmd, args)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	k8sAPI, err := k8s.InitializeAPI(
		*kubeConfigPath,
		false,
		k8s.Svc,
		k8s.NS,
		k8s.Endpoint,
	)

	//TODO: Use can-i to check for required permissions
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	secretsInformer, err := initLocalResourceInformer(k8sAPI.Client, *namespace, k8s.Secret)
	if err != nil {
		log.Fatalf("Failed to initialize secret informer: %s", err)
	}
	svcInformer, err := initLocalResourceInformer(k8sAPI.Client, *namespace, k8s.Svc)

	if err != nil {
		log.Fatalf("Failed to initialize service informer: %s", err)
	}

	probeManager := NewProbeManager(svcInformer)
	probeManager.Start()

	k8sAPI.Sync(nil)
	watcher := NewRemoteClusterConfigWatcher(*namespace, secretsInformer, k8sAPI, *requeueLimit, *repairPeriod)
	log.Info("Started cluster config watcher")

	go admin.StartServer(*metricsAddr)

	<-stop
	log.Info("Stopping cluster config watcher")
	watcher.Stop()
	probeManager.Stop()
}
