package externalworkload

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	ewv1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1beta1"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/workqueue"
	endpointslicerec "k8s.io/endpointslice"
	epsliceutil "k8s.io/endpointslice/util"
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

// EndpointsController reconciles service memberships for ExternalWorkload resources
// by writing EndpointSlice objects for Services that select one or more
// external endpoints.
type EndpointsController struct {
	k8sAPI     *k8s.API
	log        *logging.Entry
	queue      workqueue.TypedRateLimitingInterface[string]
	reconciler *endpointsReconciler
	stop       chan struct{}

	lec leaderelection.LeaderElectionConfig
	informerHandlers
	dropsMetric workqueue.CounterMetric
}

// informerHandlers holds handles to callbacks that have been registered with
// the API Server client's informers.
//
// These callbacks will be registered when a controller is elected as leader,
// and de-registered when the lease is lost.
type informerHandlers struct {
	ewHandle  cache.ResourceEventHandlerRegistration
	esHandle  cache.ResourceEventHandlerRegistration
	svcHandle cache.ResourceEventHandlerRegistration

	// Mutex to guard handler registration since the elector loop may start
	// executing callbacks when a controller starts reading in a background task
	sync.Mutex
}

// The EndpointsController code has been structured (and modified) based on the
// core EndpointSlice controller. Copyright 2014 The Kubernetes Authors
// https://github.com/kubernetes/kubernetes/blob/29fad383dab0dd7b7b563ec9eae10156616a6f34/pkg/controller/endpointslice/endpointslice_controller.go
//
// There are some fundamental differences between the core endpoints controller
// and Linkerd's endpoints controller; for one, the churn rate is expected to be
// much lower for a controller that reconciles ExternalWorkload resources.
// Furthermore, the structure of the resource is different, statuses do not
// contain as many conditions, and the lifecycle of an ExternalWorkload is
// different to that of a Pod (e.g. a workload is long lived).
//
// NewEndpointsController creates a new controller. The controller must be
// started with its `Start()` method.
func NewEndpointsController(k8sAPI *k8s.API, hostname, controllerNs string, stopCh chan struct{}, exportQueueMetrics bool) (*EndpointsController, error) {
	queueName := "endpoints_controller_workqueue"
	workQueueConfig := workqueue.TypedRateLimitingQueueConfig[string]{
		Name: queueName,
	}

	var dropsMetric workqueue.CounterMetric = &noopCounterMetric{}
	if exportQueueMetrics {
		provider := newWorkQueueMetricsProvider()
		workQueueConfig.MetricsProvider = provider
		dropsMetric = provider.NewDropsMetric(queueName)
	}

	ec := &EndpointsController{
		k8sAPI:     k8sAPI,
		reconciler: newEndpointsReconciler(k8sAPI, managedBy, maxEndpointsQuota),
		queue:      workqueue.NewTypedRateLimitingQueueWithConfig[string](workqueue.DefaultTypedControllerRateLimiter[string](), workQueueConfig),
		stop:       stopCh,
		log: logging.WithFields(logging.Fields{
			"component": "external-endpoints-controller",
		}),
		dropsMetric: dropsMetric,
	}

	// Store configuration for leader elector client. The leader elector will
	// accept three callbacks. When a lease is claimed, the elector will mark
	// the manager as a 'leader'. When a lease is released, the elector will set
	// the isLeader value back to false.
	ec.lec = leaderelection.LeaderElectionConfig{
		// When runtime context is cancelled, lock will be released. Implies any
		// code guarded by the lease _must_ finish before cancelling.
		ReleaseOnCancel: true,
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      leaseName,
				Namespace: controllerNs,
			},
			Client: k8sAPI.Client.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: hostname,
			},
		},
		LeaseDuration: leaseDuration,
		RenewDeadline: leaseRenewDeadline,
		RetryPeriod:   leaseRetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				err := ec.addHandlers()
				if err != nil {
					// If the leader has failed to register callbacks then
					// panic; we are in a bad state that's hard to recover from
					// gracefully.
					panic(fmt.Sprintf("failed to register event handlers: %v", err))
				}
			},
			OnStoppedLeading: func() {
				err := ec.removeHandlers()
				if err != nil {
					// If the leader has failed to de-register callbacks then
					// panic; otherwise, we risk racing with the newly elected
					// leader
					panic(fmt.Sprintf("failed to de-register event handlers: %v", err))
				}
				ec.log.Infof("%s released lease", hostname)
			},
			OnNewLeader: func(identity string) {
				if identity == hostname {
					ec.log.Infof("%s acquired lease", hostname)
				}
			},
		},
	}

	return ec, nil
}

// addHandlers will register a set of callbacks with the different informers
// needed to synchronise endpoint state.
func (ec *EndpointsController) addHandlers() error {
	var err error
	ec.Lock()
	defer ec.Unlock()

	// Wipe out previously observed state. This ensures we will not have stale
	// cache errors due to events that happened when callbacks were not firing.
	ec.reconciler.endpointTracker = epsliceutil.NewEndpointSliceTracker()

	ec.svcHandle, err = ec.k8sAPI.Svc().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ec.onServiceUpdate,
		DeleteFunc: ec.onServiceUpdate,
		UpdateFunc: func(_, newObj interface{}) {
			ec.onServiceUpdate(newObj)
		},
	})

	if err != nil {
		return err
	}

	ec.esHandle, err = ec.k8sAPI.ES().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ec.onEndpointSliceAdd,
		UpdateFunc: ec.onEndpointSliceUpdate,
		DeleteFunc: ec.onEndpointSliceDelete,
	})

	if err != nil {
		return err
	}

	ec.ewHandle, err = ec.k8sAPI.ExtWorkload().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ec.onAddExternalWorkload,
		DeleteFunc: ec.onDeleteExternalWorkload,
		UpdateFunc: ec.onUpdateExternalWorkload,
	})

	if err != nil {
		return err
	}

	return nil
}

// removeHandlers will de-register callbacks
func (ec *EndpointsController) removeHandlers() error {
	var err error
	ec.Lock()
	defer ec.Unlock()
	if ec.svcHandle != nil {
		if err = ec.k8sAPI.Svc().Informer().RemoveEventHandler(ec.svcHandle); err != nil {
			return err
		}
	}

	if ec.ewHandle != nil {
		if err = ec.k8sAPI.ExtWorkload().Informer().RemoveEventHandler(ec.ewHandle); err != nil {
			return err
		}
	}

	if ec.esHandle != nil {
		if err = ec.k8sAPI.ES().Informer().RemoveEventHandler(ec.esHandle); err != nil {
			return err
		}
	}

	return nil
}

// Start will run the endpoint manager's processing loop and leader elector.
//
// The function will spawn three background tasks; one to run the leader elector
// client, one that will process updates applied by the informer
// callbacks and one to handle shutdown signals and propagate them to all
// components.
//
// Warning: Do not call Start() more than once
func (ec *EndpointsController) Start() {
	// Create a parent context that will be used by leader elector to gracefully
	// shutdown.
	//
	// When cancelled (either through cancel function or by having its done
	// channel closed), the leader elector will release the lease and stop its
	// execution.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			// Block until a lease is acquired or a lease has been released
			leaderelection.RunOrDie(ctx, ec.lec)
			// If the context has been cancelled, exit the function, otherwise
			// continue spinning.
			select {
			case <-ctx.Done():
				ec.log.Trace("leader election client received shutdown signal")
				return
			default:
			}
		}
	}()

	// When a shutdown signal is received over the manager's stop channel, it is
	// propagated to the elector through the context object and to the queue
	// through its dedicated `Shutdown()` function.
	go func() {
		// Block until a shutdown signal arrives
		<-ec.stop
		// Drain the queue before signalling the lease to terminate
		ec.queue.ShutDownWithDrain()
		// Propagate shutdown to elector
		cancel()
		ec.log.Infof("received shutdown signal")
	}()

	// Start a background task to process updates.
	go ec.processQueue()
}

// processQueue spins and pops elements off the queue. When the queue has
// received a shutdown signal it exists.
//
// The queue uses locking internally so this function is thread safe and can
// have many workers call it in parallel; workers will not process the same item
// at the same time.
func (ec *EndpointsController) processQueue() {
	for {
		key, quit := ec.queue.Get()
		if quit {
			ec.log.Trace("queue received shutdown signal")
			return
		}

		err := ec.syncService(key)
		ec.handleError(err, key)

		// Tell the queue that we are done processing this key. This will
		// unblock the key for other workers to process if executing in
		// parallel, or if it needs to be re-queued because another update has
		// been received.
		ec.queue.Done(key)
	}
}

// handleError will look at the result of the queue update processing step and
// decide whether an update should be re-tried or marked as done.
//
// The queue operates with an error budget. When exceeded, the item is evicted
// from the queue (and its retry history wiped). Otherwise, the item is enqueued
// according to the queue's rate limiting algorithm.
func (ec *EndpointsController) handleError(err error, key string) {
	if err == nil {
		// Wipe out rate limiting history for key when processing was successful.
		// Next time this key is used, it will get its own fresh rate limiter
		// error budget
		ec.queue.Forget(key)
		return
	}

	if ec.queue.NumRequeues(key) < maxRetryBudget {
		ec.queue.AddRateLimited(key)
		return
	}

	ec.queue.Forget(key)
	ec.dropsMetric.Inc()
	ec.log.Errorf("dropped Service %s out of update queue: %v", key, err)
}

// syncService will run a reconciliation function for a single Service object
// that needs to have its EndpointSlice objects reconciled.
func (ec *EndpointsController) syncService(update string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(update)
	if err != nil {
		return err
	}

	svc, err := ec.k8sAPI.Svc().Lister().Services(namespace).Get(name)
	if err != nil {
		// If the error is anything except a 'NotFound' then bubble up the error
		// and re-queue the entry; the service will be re-processed at some
		// point in the future.
		if !kerrors.IsNotFound(err) {
			return err
		}

		ec.reconciler.endpointTracker.DeleteService(namespace, name)
		// The service has been deleted, return nil so that it won't be retried.
		return nil
	}

	if svc.Spec.Type == corev1.ServiceTypeExternalName {
		// services with Type ExternalName do not receive any endpoints
		return nil
	}

	if svc.Spec.Selector == nil {
		// services without a selector will not get any endpoints automatically
		// created; this is done out-of-band by the service operator
		return nil
	}

	ewSelector := labels.Set(svc.Spec.Selector).AsSelectorPreValidated()
	ews, err := ec.k8sAPI.ExtWorkload().Lister().List(ewSelector)
	if err != nil {
		// This operation should be infallible since we retrieve from the cache
		// (we can guarantee we will receive at least an empty list), for good
		// measure, bubble up the error if one will be returned by the informer.
		return err
	}

	esSelector := labels.Set(map[string]string{
		discoveryv1.LabelServiceName: svc.Name,
		discoveryv1.LabelManagedBy:   managedBy,
	}).AsSelectorPreValidated()
	epSlices, err := ec.k8sAPI.ES().Lister().List(esSelector)
	if err != nil {
		return err
	}

	epSlices = dropEndpointSlicesPendingDeletion(epSlices)
	if ec.reconciler.endpointTracker.StaleSlices(svc, epSlices) {
		ec.log.Warnf("detected EndpointSlice informer cache is out of date when processing %s", update)
		return errors.New("EndpointSlice informer cache is out of date")
	}
	err = ec.reconciler.reconcile(svc, ews, epSlices)
	if err != nil {
		return err
	}

	return nil
}

// When a service update has been received (regardless of the event type, i.e.
// can be Added, Modified, Deleted) send it to the endpoint controller for
// processing.
func (ec *EndpointsController) onServiceUpdate(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		ec.log.Infof("failed to get key for object %+v: %v", obj, err)
		return
	}

	namespace, _, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		ec.log.Infof("failed to get namespace from key %s: %v", key, err)
	}

	// Skip processing 'core' services
	if namespace == "kube-system" {
		return
	}

	ec.queue.Add(key)
}

// onEndpointSliceAdd queues a sync for the relevant Service for a sync if the
// EndpointSlice resource version does not match the expected version in the
// endpointSliceTracker.
func (ec *EndpointsController) onEndpointSliceAdd(obj interface{}) {
	es := obj.(*discoveryv1.EndpointSlice)
	if es == nil {
		ec.log.Info("Invalid EndpointSlice provided to onEndpointSliceAdd()")
		return
	}

	if managedByController(es) && ec.reconciler.endpointTracker.ShouldSync(es) {
		ec.queueServiceForEndpointSlice(es)
	}
}

// onEndpointSliceUpdate queues a sync for the relevant Service for a sync if
// the EndpointSlice resource version does not match the expected version in the
// endpointSliceTracker or the managed-by value of the EndpointSlice has changed
// from or to this controller.
func (ec *EndpointsController) onEndpointSliceUpdate(prevObj, obj interface{}) {
	prevEndpointSlice := prevObj.(*discoveryv1.EndpointSlice)
	endpointSlice := obj.(*discoveryv1.EndpointSlice)
	if endpointSlice == nil || prevEndpointSlice == nil {
		ec.log.Info("Invalid EndpointSlice provided to onEndpointSliceUpdate()")
		return
	}

	// EndpointSlice generation does not change when labels change. Although the
	// controller will never change LabelServiceName, users might. This check
	// ensures that we handle changes to this label.
	svcName := endpointSlice.Labels[discoveryv1.LabelServiceName]
	prevSvcName := prevEndpointSlice.Labels[discoveryv1.LabelServiceName]
	if svcName != prevSvcName {
		ec.log.Infof("label changed label: %s, oldService: %s, newService: %s, endpointsliece: %s", discoveryv1.LabelServiceName, prevSvcName, svcName, endpointSlice.Name)
		ec.queueServiceForEndpointSlice(endpointSlice)
		ec.queueServiceForEndpointSlice(prevEndpointSlice)
		return
	}
	if managedByChanged(prevEndpointSlice, endpointSlice) ||
		(managedByController(endpointSlice) && ec.reconciler.endpointTracker.ShouldSync(endpointSlice)) {
		ec.queueServiceForEndpointSlice(endpointSlice)
	}
}

// onEndpointSliceDelete queues a sync for the relevant Service for a sync if the
// EndpointSlice resource version does not match the expected version in the
// endpointSliceTracker.
func (ec *EndpointsController) onEndpointSliceDelete(obj interface{}) {
	endpointSlice := ec.getEndpointSliceFromDeleteAction(obj)
	if endpointSlice != nil && managedByController(endpointSlice) && ec.reconciler.endpointTracker.Has(endpointSlice) {
		// This returns false if we didn't expect the EndpointSlice to be
		// deleted. If that is the case, we queue the Service for another sync.
		if !ec.reconciler.endpointTracker.HandleDeletion(endpointSlice) {
			ec.queueServiceForEndpointSlice(endpointSlice)
		}
	}
}

// queueServiceForEndpointSlice attempts to queue the corresponding Service for
// the provided EndpointSlice.
func (ec *EndpointsController) queueServiceForEndpointSlice(endpointSlice *discoveryv1.EndpointSlice) {
	key, err := endpointslicerec.ServiceControllerKey(endpointSlice)
	if err != nil {
		ec.log.Errorf("Couldn't get key for EndpointSlice %+v: %v", endpointSlice, err)
		return
	}

	ec.queue.Add(key)
}

func (ec *EndpointsController) onAddExternalWorkload(obj interface{}) {
	ew, ok := obj.(*ewv1beta1.ExternalWorkload)
	if !ok {
		ec.log.Errorf("couldn't get ExternalWorkload from object %#v", obj)
		return
	}

	services, err := ec.getExternalWorkloadSvcMembership(ew)
	if err != nil {
		ec.log.Errorf("failed to get service membership for %s/%s: %v", ew.Namespace, ew.Name, err)
		return
	}

	for svc := range services {
		ec.queue.Add(svc)
	}
}

func (ec *EndpointsController) onUpdateExternalWorkload(old, cur interface{}) {
	services := ec.getServicesToUpdateOnExternalWorkloadChange(old, cur)

	for svc := range services {
		ec.queue.Add(svc)
	}
}

func (ec *EndpointsController) onDeleteExternalWorkload(obj interface{}) {
	ew := ec.getExternalWorkloadFromDeleteAction(obj)
	if ew != nil {
		ec.onAddExternalWorkload(ew)
	}
}

func dropEndpointSlicesPendingDeletion(endpointSlices []*discoveryv1.EndpointSlice) []*discoveryv1.EndpointSlice {
	n := 0
	for _, endpointSlice := range endpointSlices {
		if endpointSlice.DeletionTimestamp == nil {
			endpointSlices[n] = endpointSlice
			n++
		}
	}
	return endpointSlices[:n]
}
