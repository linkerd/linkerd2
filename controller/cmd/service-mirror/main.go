package servicemirror

import (
	"context"
	"errors"
	"flag"
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

type chanProbeEventSink struct{ sender func(event interface{}) }

func (s *chanProbeEventSink) send(event interface{}) {
	s.sender(event)
}

func initLocalSecretsInformer(api kubernetes.Interface, namespace string) (cache.SharedIndexInformer, error) {
	sharedInformers := informers.NewSharedInformerFactoryWithOptions(api, 10*time.Minute, informers.WithNamespace(namespace))

	informer := sharedInformers.Core().V1().Secrets().Informer()

	sharedInformers.Start(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log.Infof("waiting for local namespaced secrets informer caches to sync")
	if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
		return nil, errors.New("failed to sync local namespaced secrets informer caches")
	}
	log.Infof("local namespaced secrets informer  caches synced")
	return informer, nil
}

// Main executes the tap service-mirror
func Main(args []string) {
	cmd := flag.NewFlagSet("service-mirror", flag.ExitOnError)

	kubeConfigPath := cmd.String("kubeconfig", "", "path to the local kube config")
	requeueLimit := cmd.Int("event-requeue-limit", 3, "requeue limit for events")
	metricsAddr := cmd.String("metrics-addr", ":9999", "address to serve scrapable metrics on")
	namespace := cmd.String("namespace", "", "address to serve scrapable metrics on")

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

	secretsInformer, err := initLocalSecretsInformer(k8sAPI.Client, *namespace)

	if err != nil {
		log.Fatalf("Failed to initialize secrets informer: %s", err)
	}

	probeManager := NewProbeManager(k8sAPI)
	probeManager.Start()

	k8sAPI.Sync(nil)
	watcher := NewRemoteClusterConfigWatcher(*namespace, secretsInformer, k8sAPI, *requeueLimit, &chanProbeEventSink{probeManager.enqueueEvent})
	log.Info("Started cluster config watcher")

	go admin.StartServer(*metricsAddr)

	<-stop
	log.Info("Stopping cluster config watcher")
	watcher.Stop()
	probeManager.Stop()
}
