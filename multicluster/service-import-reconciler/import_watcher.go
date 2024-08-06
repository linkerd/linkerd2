package importreconciler

import (
	"context"
	"fmt"
	"sync"
	"time"

	linkv1alpha1 "github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha1"
	smp "github.com/linkerd/linkerd2/controller/gen/apis/serviceimport/v1alpha1"
	l5dApi "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/workqueue"
)

const (
	// Name of the lease resource the controller will use
	leaseName = "linkerd-service-import-write"

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
	managedBy = "linkerd-import-reconciler-controller"

	// Max retries for a service to be reconciled
	maxRetryBudget = 15
)

type ServiceImportWatcher struct {
	// Index links by cluster name
	clusters    map[string]*remoteCluster
	localClient *k8s.API
	l5dClient   l5dApi.Interface

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
	link   *linkv1alpha1.Link
	client *k8s.API

	log *logging.Entry
	informerHandlers
}

type informerHandlers struct {
	svcHandler  cache.ResourceEventHandlerRegistration
	linkHandler cache.ResourceEventHandlerRegistration
}

func NewServiceImportWatcher(
	localAPI *k8s.API,
	l5dAPI l5dApi.Interface,
	mcNs string,
	hostname string,
	stop chan struct{},
) *ServiceImportWatcher {
	sw := &ServiceImportWatcher{
		clusters:              make(map[string]*remoteCluster),
		localClient:           localAPI,
		l5dClient:             l5dAPI,
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
				Namespace: mcNs,
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
				sw.localClient.Sync(sw.stop)
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
		sw.log.Info("received shutdown signal")
		sw.eventsQueue.ShutDownWithDrain()
		cancel()
		break
	}
	return nil
}

func (sw *ServiceImportWatcher) processQueue() {
	for {
		event, quit := sw.eventsQueue.Get()
		if quit {
			sw.log.Info("queue received shutdown signal")
			return
		}

		sw.log.Tracef("processing event (type %T) %#v", event, event)
		var err error
		switch ev := event.(type) {
		case *clusterRegistration:
			// handle registration
			err = sw.createOrUpdateCluster(ev.link)
		case *clusterUpdate:
			// handle update
			err = sw.updateOrDeleteCluster(ev.link, ev.deleted)
		case *clusterDelete:
			// final delete after all services have been processed
			sw.Lock()
			delete(sw.clusters, ev.clusterName)
			sw.Unlock()
		case *serviceUpdate:
			if ev.exported {
				err = sw.reconcileServiceImport(ev.cluster, ev.svc)
			} else {
				err = sw.removeClusterFromImport(ev.cluster, ev.svc)
			}
		}

		sw.eventsQueue.Done(event)
		if err == nil {
			sw.eventsQueue.Forget(event)
		} else {
			sw.log.Info("error when processing event: #+v", event)
		}
	}
}

func (sw *ServiceImportWatcher) removeClusterFromImport(clusterName string, service *corev1.Service) error {
	imp, err := sw.localClient.Smp().Lister().ServiceImports(service.Namespace).Get(service.Name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			// Nothing to remove
			return nil
		} else {
			return err
		}
	}

	// Second, update the service import
	updatedImp := imp.DeepCopy()
	clusters := []string{}
	for _, c := range updatedImp.Status.Clusters {
		if c != clusterName {
			clusters = append(clusters, c)
		}
	}

	updatedImp.Status.Clusters = clusters
	_, err = sw.l5dClient.ServiceimportV1alpha1().ServiceImports(service.Namespace).Update(context.TODO(), updatedImp, metav1.UpdateOptions{})
	return err
}

func (sw *ServiceImportWatcher) reconcileServiceImport(clusterName string, service *corev1.Service) error {
	imp, err := sw.localClient.Smp().Lister().ServiceImports(service.Namespace).Get(service.Name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			// Create svc import
			imp, err = sw.createServiceImport(service.Name, service.Namespace)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// Second, update the service import
	updatedImp := imp.DeepCopy()
	update := true
	for _, c := range updatedImp.Status.Clusters {
		if c == clusterName {
			update = false
		}
	}

	if !update {
		return nil
	}

	updatedImp.Status.Clusters = append(updatedImp.Status.Clusters, clusterName)
	_, err = sw.l5dClient.ServiceimportV1alpha1().ServiceImports(service.Namespace).Update(context.TODO(), updatedImp, metav1.UpdateOptions{})
	return err
}

func (sw *ServiceImportWatcher) createServiceImport(serviceName, serviceNamespace string) (*smp.ServiceImport, error) {
	svc, err := sw.localClient.Svc().Lister().Services(serviceNamespace).Get(serviceName)
	if err != nil {
		return nil, err
	}

	// TODO (matei): WARNING, we only process int ports. Port type is intorstr.
	portSpecs := []smp.PortSpec{}
	for _, pS := range svc.Spec.Ports {
		portSpecs = append(portSpecs, smp.PortSpec{
			Name:     pS.Name,
			Port:     pS.TargetPort,
			Protocol: pS.Protocol,
		})
	}
	yes := true
	imp := &smp.ServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: serviceNamespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "v1",
					Kind:               "Service",
					Name:               svc.Name,
					UID:                svc.UID,
					Controller:         &yes,
					BlockOwnerDeletion: &yes,
				},
			},
		},
		Spec: smp.ServiceImportSpec{
			Ports: portSpecs,
		},
		Status: smp.ServiceImportStatus{
			Clusters: []string{},
		},
	}
	return sw.l5dClient.ServiceimportV1alpha1().ServiceImports(serviceNamespace).Create(context.TODO(), imp, metav1.CreateOptions{})
}
