package servicemirror

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha3"
	controllerK8s "github.com/linkerd/linkerd2/controller/k8s"
	servicemirror "github.com/linkerd/linkerd2/multicluster/service-mirror"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/k8s"
	sm "github.com/linkerd/linkerd2/pkg/servicemirror"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

const (
	linkWatchRestartAfter = 10 * time.Second
	// Duration of the lease
	LEASE_DURATION = 30 * time.Second
	// Deadline for the leader to refresh its lease. Defaults to the same value
	// used by core controllers
	LEASE_RENEW_DEADLINE = 10 * time.Second
	// Duration leader elector clients should wait between action re-tries.
	// Defaults to the same value used by core controllers
	LEASE_RETRY_PERIOD = 2 * time.Second
)

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
	enableHeadlessSvc := cmd.Bool("enable-headless-services", false, "toggle support for headless service mirroring")
	enableNamespaceCreation := cmd.Bool("enable-namespace-creation", false, "toggle support for namespace creation")
	enablePprof := cmd.Bool("enable-pprof", false, "Enable pprof endpoints on the admin server")
	localMirror := cmd.Bool("local-mirror", false, "watch the local cluster for federated service members")
	federatedServiceSelector := cmd.String("federated-service-selector", k8s.DefaultFederatedServiceSelector, "Selector (label query) for federated service members in the local cluster")
	probeSvc := cmd.String("probe-service", "", "Name of the target cluster probe service")

	flags.ConfigureAndParse(cmd, args)
	linkName := cmd.Arg(0)

	ready := false
	adminServer := admin.NewServer(*metricsAddr, *enablePprof, &ready)

	go func() {
		log.Infof("starting admin server on %s", *metricsAddr)
		if err := adminServer.ListenAndServe(); err != nil {
			log.Errorf("failed to start service mirror admin server: %s", err)
		}
	}()

	rootCtx, cancel := context.WithCancel(context.Background())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		log.Info("Received shutdown signal")
		// Cancel root context. Cancellation will be propagated to all other
		// contexts that are children of the root context.
		cancel()
	}()

	// We create two different kubernetes API clients for the local cluster:
	// l5dClient is used for watching Link resources and updating their
	// statuses.
	//
	// controllerK8sAPI is used by the cluster watcher to manage
	// mirror resources such as services, namespaces, and endpoints.

	controllerK8sAPI, err := controllerK8s.InitializeAPI(
		rootCtx,
		*kubeConfigPath,
		false,
		"local",
		controllerK8s.NS,
		controllerK8s.Svc,
		controllerK8s.Endpoint,
	)
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}
	config, err := k8s.GetConfig(*kubeConfigPath, "")
	if err != nil {
		log.Fatalf("error configuring Kubernetes API client: %s", err)
	}
	l5dClient, err := controllerK8s.NewL5DCRDClient(config)
	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	metrics := servicemirror.NewProbeMetricVecs()
	controllerK8sAPI.Sync(nil)
	ready = true

	linksAPI := controllerK8s.NewL5dNamespacedAPI(l5dClient, *namespace, "local", controllerK8s.Link)
	log.Infof("Starting Link informer")
	linksAPI.Sync(rootCtx.Done())

	var run func(context.Context)

	if *localMirror {
		run = func(ctx context.Context) {
			err = startLocalClusterWatcher(ctx, *namespace, controllerK8sAPI, linksAPI, *requeueLimit, *repairPeriod, *enableHeadlessSvc, *enableNamespaceCreation, *federatedServiceSelector)
			if err != nil {
				log.Fatalf("Failed to start local cluster watcher: %s", err)
			}

			// ctx.Done() is a one-shot channel that will be closed once
			// the context has been cancelled. Receiving from a closed
			// channel yields the value immediately.
			<-ctx.Done()
			// The channel will be closed by the leader elector when a
			// lease is lost, or by a background task handling SIGTERM.
			// Before terminating the loop, stop the workers and set
			// them to nil to release memory.
			cleanupWorkers()
		}
	} else {
		run = func(ctx context.Context) {
			// Use a small buffered channel for Link updates to avoid dropping
			// updates if there is an update burst.
			results := make(chan *v1alpha3.Link, 100)

			_, err := linksAPI.Link().Informer().AddEventHandler(servicemirror.GetLinkHandlers(results, linkName))

			if err != nil {
				log.Fatalf("Failed to add event handler to Link informer: %s", err)
			}

			// Each time the link resource is updated, reload the config and restart the
			// cluster watcher.
			for {
				select {
				// ctx.Done() is a one-shot channel that will be closed once
				// the context has been cancelled. Receiving from a closed
				// channel yields the value immediately.
				case <-ctx.Done():
					// The channel will be closed by the leader elector when a
					// lease is lost, or by a background task handling SIGTERM.
					// Before terminating the loop, stop the workers and set
					// them to nil to release memory.
					cleanupWorkers()
				case link := <-results:
					if link != nil {
						log.Infof("Got updated link %s: %+v", linkName, link)
						creds, err := loadCredentials(ctx, link, *namespace, controllerK8sAPI.Client)
						if err != nil {
							log.Errorf("Failed to load remote cluster credentials: %s", err)
						}
						err = restartClusterWatcher(ctx, link, *namespace, *probeSvc, creds, controllerK8sAPI, linksAPI, *requeueLimit, *repairPeriod, metrics, *enableHeadlessSvc, *enableNamespaceCreation)
						if err != nil {
							// failed to restart cluster watcher; give a bit of slack
							// and requeue the link to give it another try
							log.Error(err)
							time.Sleep(linkWatchRestartAfter)
							results <- link
						}
					} else {
						log.Infof("Link %s deleted", linkName)
						cleanupWorkers()
					}
				}
			}
		}
	}

	hostname, found := os.LookupEnv("HOSTNAME")
	if !found {
		log.Fatal("Failed to fetch 'HOSTNAME' environment variable")
	}

	leaseName := fmt.Sprintf("service-mirror-write-%s", linkName)
	if *localMirror {
		leaseName = "local-service-mirror-write"
	}
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      leaseName,
			Namespace: *namespace,
		},
		Client: controllerK8sAPI.Client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: hostname,
		},
	}

election:
	for {
		// RunOrDie will block until the lease is lost.
		//
		// When a lease is acquired, the OnStartedLeading callback will be
		// triggered, and a main watcher loop will be established to watch Link
		// resources.
		//
		// When the lease is lost, all watchers will be cleaned-up and we will
		// loop then attempt to re-acquire the lease.
		leaderelection.RunOrDie(rootCtx, leaderelection.LeaderElectionConfig{
			// When runtime context is cancelled, lock will be released. Implies any
			// code guarded by the lease _must_ finish before cancelling.
			ReleaseOnCancel: true,
			Lock:            lock,
			LeaseDuration:   LEASE_DURATION,
			RenewDeadline:   LEASE_RENEW_DEADLINE,
			RetryPeriod:     LEASE_RETRY_PERIOD,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: func(ctx context.Context) {
					// When a lease is lost, RunOrDie will cancel the context
					// passed into the OnStartedLeading callback. This will in
					// turn cause us to cancel the work in the run() function,
					// effectively terminating and cleaning-up the watches.
					log.Info("Starting controller loop")
					run(ctx)
				},
				OnStoppedLeading: func() {
					log.Infof("%s released lease", hostname)
				},
				OnNewLeader: func(identity string) {
					if identity == hostname {
						log.Infof("%s acquired lease", hostname)
					}
				},
			},
		})

		select {
		// If the lease has been lost, and we have received a shutdown signal,
		// break the loop and gracefully exit. We can guarantee at this point
		// resources have been released.
		case <-rootCtx.Done():
			break election
		// If the lease has been lost, loop and attempt to re-acquire it.
		default:

		}
	}
	log.Info("Shutting down")
}

// cleanupWorkers is a utility function that checks whether the worker pointers
// (clusterWatcher and probeWorker) are instantiated, and if they are, stops
// their execution and sets the pointers to a nil value so that memory may be
// garbage collected.
func cleanupWorkers() {
	if clusterWatcher != nil {
		// release, but do not clean-up services created
		// the `unlink` command will take care of that
		clusterWatcher.Stop(false)
		clusterWatcher = nil
	}

	if probeWorker != nil {
		probeWorker.Stop()
		probeWorker = nil
	}
}

func loadCredentials(ctx context.Context, link *v1alpha3.Link, namespace string, k8sAPI kubernetes.Interface) ([]byte, error) {
	// Load the credentials secret
	secret, err := k8sAPI.CoreV1().Secrets(namespace).Get(ctx, link.Spec.ClusterCredentialsSecret, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to load credentials secret %s: %w", link.Spec.ClusterCredentialsSecret, err)
	}
	return sm.ParseRemoteClusterSecret(secret)
}

func restartClusterWatcher(
	ctx context.Context,
	link *v1alpha3.Link,
	namespace,
	probeSvc string,
	creds []byte,
	controllerK8sAPI *controllerK8s.API,
	linksAPI *controllerK8s.API,
	requeueLimit int,
	repairPeriod time.Duration,
	metrics servicemirror.ProbeMetricVecs,
	enableHeadlessSvc bool,
	enableNamespaceCreation bool,
) error {

	cleanupWorkers()

	workerMetrics, err := metrics.NewWorkerMetrics(link.Spec.TargetClusterName)
	if err != nil {
		return fmt.Errorf("failed to create metrics for cluster watcher: %w", err)
	}

	// If linked against a cluster that has a gateway, start a probe and
	// initialise the liveness channel
	var ch chan bool
	if link.Spec.ProbeSpec.Path != "" {
		probeWorker = servicemirror.NewProbeWorker(probeSvc, &link.Spec.ProbeSpec, workerMetrics, link.Spec.TargetClusterName)
		probeWorker.Start()
		ch = probeWorker.Liveness
	}

	// Start cluster watcher
	cfg, err := clientcmd.RESTConfigFromKubeConfig(creds)
	if err != nil {
		return fmt.Errorf("unable to parse kube config: %w", err)
	}
	remoteAPI, err := controllerK8s.InitializeAPIForConfig(ctx, cfg, false, link.Spec.TargetClusterName, controllerK8s.Svc, controllerK8s.Endpoint)
	if err != nil {
		return fmt.Errorf("cannot initialize api for target cluster %s: %w", link.Spec.TargetClusterName, err)
	}
	cw, err := servicemirror.NewRemoteClusterServiceWatcher(
		ctx,
		namespace,
		controllerK8sAPI,
		remoteAPI,
		linksAPI,
		probeSvc,
		link,
		requeueLimit,
		repairPeriod,
		ch,
		enableHeadlessSvc,
		enableNamespaceCreation,
	)
	if err != nil {
		return fmt.Errorf("unable to create cluster watcher: %w", err)
	}
	clusterWatcher = cw
	err = clusterWatcher.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start cluster watcher: %w", err)
	}

	return nil
}

func startLocalClusterWatcher(
	ctx context.Context,
	namespace string,
	controllerK8sAPI *controllerK8s.API,
	linksAPI *controllerK8s.API,
	requeueLimit int,
	repairPeriod time.Duration,
	enableHeadlessSvc bool,
	enableNamespaceCreation bool,
	federatedServiceSelector string,
) error {
	federatedLabelSelector, err := metav1.ParseToLabelSelector(federatedServiceSelector)
	if err != nil {
		return fmt.Errorf("failed to parse federated service selector: %w", err)
	}

	link := v1alpha3.Link{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "local",
			Namespace: namespace,
		},
		Spec: v1alpha3.LinkSpec{
			TargetClusterName:        "",
			Selector:                 nil,
			RemoteDiscoverySelector:  nil,
			FederatedServiceSelector: federatedLabelSelector,
		},
	}
	cw, err := servicemirror.NewRemoteClusterServiceWatcher(
		ctx,
		namespace,
		controllerK8sAPI,
		controllerK8sAPI,
		linksAPI,
		"",
		&link,
		requeueLimit,
		repairPeriod,
		make(chan bool),
		enableHeadlessSvc,
		enableNamespaceCreation,
	)
	if err != nil {
		return fmt.Errorf("unable to create cluster watcher: %w", err)
	}
	clusterWatcher = cw
	err = clusterWatcher.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start cluster watcher: %w", err)
	}

	return nil
}
