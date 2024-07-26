package importreconciler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/multicluster"
	sm "github.com/linkerd/linkerd2/pkg/servicemirror"
	logging "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/workqueue"
)

const (
	// Name of the lease resource the controller will use
	leaseName = "linkerd-destination-endpoint-write"

	// Duration of the lease
	// Core controllers (kube-controller-manager) has a duration of 15 seconds
	leaseDuration = 30 * time.Second

	// Deadline for the leader to refresh its lease. Core controllers have a
	// deadline of 10 seconds.
	leaseRenewDeadline = 10 * time.Second

	// Duration a leader elector should wait in between action re-tries.
	// Core controllers have a value of 2 seconds.
	leaseRetryPeriod = 2 * time.Second

	// Name of the controller. Used as an annotation value for all created
	// EndpointSlice objects
	managedBy = "linkerd-external-workloads-controller"

	// Max number of endpoints per EndpointSlice
	maxEndpointsQuota = 100

	// Max retries for a service to be reconciled
	maxRetryBudget = 15
)

type ServiceImportWatcher struct {
	// Index links by cluster name
	clusters              map[string]*remoteCluster
	localClient           *k8s.API
	multiclusterNamespace string

	informerHandlers
	stop chan struct{}
	log  *logging.Entry

	eventsQueue workqueue.RateLimitingInterface
	lec         leaderelection.LeaderElectionConfig

	sync.RWMutex
}

type remoteCluster struct {
	name   string
	link   *multicluster.Link
	client *k8s.API

	informerHandlers
}

type informerHandlers struct {
	svcHandler  cache.ResourceEventHandlerRegistration
	linkHandler cache.ResourceEventHandlerRegistration
}

/* Events */
type (
	clusterRegistered struct {
		link *multicluster.Link
	}
	clusterUpdated struct {
		link *multicluster.Link
	}
)

func NewServiceImportWatcher(
	localAPI *k8s.API,
	mcNs string,
	controllerNs string,
	hostname string,
	stop chan struct{},
) *ServiceImportWatcher {
	sw := &ServiceImportWatcher{
		clusters:              make(map[string]*remoteCluster),
		localClient:           localAPI,
		multiclusterNamespace: mcNs,
		stop:                  stop,
		log: logging.WithFields(logging.Fields{
			"component": "service-import-reconciler",
		}),
		eventsQueue: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	sw.lec = leaderelection.LeaderElectionConfig{
		// When runtime context is cancelled, lock will be released. Implies any
		// code guarded by the lease _must_ finish before cancelling.
		ReleaseOnCancel: true,
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      leaseName,
				Namespace: controllerNs,
			},
			Client: sw.localClient.Client.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: hostname,
			},
		},
		LeaseDuration: leaseDuration,
		RenewDeadline: leaseRenewDeadline,
		RetryPeriod:   leaseRetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				err := sw.registerCallbacks()
				if err != nil {
					// If the leader has failed to register callbacks then
					// panic; we are in a bad state that's hard to recover from
					// gracefully.
					panic(fmt.Sprintf("failed to register event handlers: %v", err))
				}
			},
			OnStoppedLeading: func() {
				err := sw.deregisterCallbacks()
				if err != nil {
					// If the leader has failed to de-register callbacks then
					// panic; otherwise, we risk racing with the newly elected
					// leader
					panic(fmt.Sprintf("failed to de-register event handlers: %v", err))
				}
				sw.log.Infof("%s released lease", hostname)
			},
			OnNewLeader: func(identity string) {
				if identity == hostname {
					sw.log.Infof("%s acquired lease", hostname)
				}
			},
		},
	}
	return sw
}

func (sw *ServiceImportWatcher) Run() error {
	sw.log.Info("starting ImportWatcher")
	if err := sw.registerCallbacks(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			leaderelection.RunOrDie(ctx, sw.lec)
			select {
			case <-ctx.Done():
				sw.log.Info("leader election client received shutdown signal")
				return
			default:
			}
		}
	}()

	go sw.processQueue()
	for {
		<-sw.stop
		sw.eventsQueue.ShutDownWithDrain()
		cancel()
		sw.log.Info("received shutdown signal")
	}
}

func (sw *ServiceImportWatcher) processQueue() {
	for {
		event, quit := sw.eventsQueue.Get()
		if quit {
			sw.log.Info("queue received shutdown signal")
			return
		}

		var err error
		switch ev := event.(type) {
		case *clusterRegistered:
			// handle registration
			err = sw.registerCluster(ev.link)
		case *clusterUpdated:
			// handle update
		}

		sw.eventsQueue.Done(event)
		if err == nil {
			sw.eventsQueue.Forget(event)
		} else {
			sw.log.Info("error when processing event: #+v", event)
		}
	}
}

func (sw *ServiceImportWatcher) registerCluster(link *multicluster.Link) error {
	secret, err := sw.localClient.Client.CoreV1().Secrets(sw.multiclusterNamespace).Get(context.TODO(), link.ClusterCredentialsSecret, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to load credentials secret %s: %w", link.ClusterCredentialsSecret, err)
	}
	creds, err := sm.ParseRemoteClusterSecret(secret)
	if err != nil {
		return fmt.Errorf("failed to parse credentials %s: %w", link.Name, err)
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig(creds)
	if err != nil {
		return fmt.Errorf("failed to parse kube config %s: %w", link.Name, err)
	}

	remoteAPI, err := k8s.InitializeAPIForConfig(context.TODO(), cfg, false, link.TargetClusterName, k8s.Svc)
	if err != nil {
		return fmt.Errorf("cannot initialize api for target cluster %s: %w", link.TargetClusterName, err)
	}

	svcHandler, err := remoteAPI.Svc().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
		},
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
		},
		DeleteFunc: func(obj interface{}) {
		},
	})
	if err != nil {
		return fmt.Errorf("failed to register callbacks for link %s: %w", link.Name, err)
	}

	cluster := &remoteCluster{
		name:   link.TargetClusterName,
		link:   link,
		client: remoteAPI,
	}
	sw.svcHandler = svcHandler

	sw.Lock()
	defer sw.Unlock()
	sw.clusters[cluster.name] = cluster
	return nil
}

func (sw *ServiceImportWatcher) registerCallbacks() error {
	sw.Lock()
	defer sw.Unlock()
	var err error
	sw.linkHandler, err = sw.localClient.Link().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(link interface{}) {
			sw.eventsQueue.Add(&clusterRegistered{link.(*multicluster.Link)})
		},
		UpdateFunc: func(_, newL interface{}) {
			sw.eventsQueue.Add(&clusterUpdated{newL.(*multicluster.Link)})
		},
		DeleteFunc: func(link interface{}) {
			sw.log.Info("delete not implemented")
		},
	})
	return err
}

func (sw *ServiceImportWatcher) deregisterCallbacks() error {
	sw.Lock()
	defer sw.Unlock()
	var err error
	if sw.linkHandler != nil {
		err = sw.localClient.Link().Informer().RemoveEventHandler(sw.linkHandler)
		if err != nil {
			return err
		}
	}
	return nil
}
