package externalworkload

import (
	"context"
	"sync"
	"time"

	ewv1alpha1 "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1alpha1"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

const (
	// Specifies capacity for updates buffer
	updateQueueCapacity = 400

	// Name of the lease resource the controller will use
	leaseName = "linkerd-destination-endpoint-write"

	// Duration of the lease
	leaseDuration = 30 * time.Second

	// Deadline for the leader to refresh its lease. Core controllers have a
	// deadline of 10 seconds.
	leaseRenewDeadline = 10 * time.Second

	// Duration a leader elector should wait in between action re-tries.
	// Core controllers have a value of 2 seconds.
	leaseRetryPeriod = 2 * time.Second
)

type (
	queueMetrics struct {
		queueUpdates prometheus.Counter
		queueDrops   prometheus.Counter
		queueLatency prometheus.Histogram

		queueLength prometheus.GaugeFunc
	}

	queueUpdate struct {
		item string

		enqueTime time.Time
	}
)

var (
	queueUpdateCounter = promauto.NewCounter(prometheus.CounterOpts{
		Name: "external_endpoints_controller_queue_updates",
		Help: "Total number of updates that entered the queue",
	})

	queueDroppedCounter = promauto.NewCounter(prometheus.CounterOpts{
		Name: "external_endpoints_controller_queue_dropped",
		Help: "Total number of updates dropped due to a backed-up queue",
	})

	queueLatencyHist = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "external_endpoints_controller_queue_latency_seconds",
		Help:    "Distribution of durations that updates have spent in the queue",
		Buckets: []float64{.005, .01, .05, .1, .5, 1, 3, 10},
	})
)

// EndpointsController reconciles service memberships for ExternalWorkload resources
// by writing EndpointSlice objects for Services that select one or more
// external endpoints.
type EndpointsController struct {
	k8sAPI   *k8s.API
	log      *logging.Entry
	updates  chan queueUpdate
	stop     chan struct{}
	isLeader bool

	lec leaderelection.LeaderElectionConfig
	queueMetrics
	sync.RWMutex
}

func NewEndpointsController(k8sAPI *k8s.API, hostname, controllerNs string, stopCh chan struct{}) (*EndpointsController, error) {
	ec := &EndpointsController{
		k8sAPI:  k8sAPI,
		updates: make(chan queueUpdate, updateQueueCapacity),
		queueMetrics: queueMetrics{
			queueUpdates: queueUpdateCounter,
			queueDrops:   queueDroppedCounter,
			queueLatency: queueLatencyHist,
		},
		stop: stopCh,
		log: logging.WithFields(logging.Fields{
			"component": "external-endpoints-controller",
		}),
	}

	ec.queueMetrics.queueLength = promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "external_endpoints_controller_queue_length",
		Help: "Total number of updates currently waiting to be processed",
	}, func() float64 { return (float64)(len(ec.updates)) })

	_, err := k8sAPI.Svc().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ec.updateService,
		DeleteFunc: ec.updateService,
		UpdateFunc: func(_, newObj interface{}) {
			ec.updateService(newObj)
		},
	})

	if err != nil {
		return nil, err
	}

	_, err = k8sAPI.ExtWorkload().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ew, ok := obj.(*ewv1alpha1.ExternalWorkload)
			if !ok {
				ec.log.Errorf("error processing ExternalWorkload event: expected *v1alpha1.ExternalWorkload, got %#v", obj)
				return
			}

			if len(ew.Spec.WorkloadIPs) == 0 {
				ec.log.Debugf("skipping ExternalWorkload event: %s/%s has no IP addresses", ew.Namespace, ew.Name)
				return
			}

			if len(ew.Spec.Ports) == 0 {
				ec.log.Debugf("skipping ExternalWorkload event: %s/%s has no ports", ew.Namespace, ew.Name)
				return
			}

			services, err := ec.getSvcMembership(ew)
			if err != nil {
				ec.log.Errorf("failed to get service membership for %s/%s: %v", ew.Namespace, ew.Name, err)
				return
			}

			for svc := range services {
				ec.enqueueUpdate(svc)
			}
		},
		DeleteFunc: func(obj interface{}) {
			var ew *ewv1alpha1.ExternalWorkload
			if ew, ok := obj.(*ewv1alpha1.ExternalWorkload); ok {
				services, err := ec.getSvcMembership(ew)
				if err != nil {
					ec.log.Errorf("failed to get service membership for %s/%s: %v", ew.Namespace, ew.Name, err)
					return
				}

				for svc := range services {
					ec.enqueueUpdate(svc)
				}
				return
			}

			tomb, ok := obj.(cache.DeletedFinalStateUnknown)
			if !ok {
				ec.log.Errorf("error processing ExternalWorkload event: couldn't retrieve obj from DeletedFinalStateUnknown %#v", obj)
				return
			}

			ew, ok = tomb.Obj.(*ewv1alpha1.ExternalWorkload)
			if !ok {
				ec.log.Errorf("error processing ExternalWorkload event: DeletedFinalStateUnknown contained object that is not a v1alpha1.ExternalWorkload %#v", obj)
				return
			}

			services, err := ec.getSvcMembership(ew)
			if err != nil {
				ec.log.Errorf("failed to get service membership for %s/%s: %v", ew.Namespace, ew.Name, err)
				return
			}

			for svc := range services {
				ec.enqueueUpdate(svc)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			old, ok := oldObj.(*ewv1alpha1.ExternalWorkload)
			if !ok {
				ec.log.Errorf("error processing ExternalWorkload event: expected *v1alpha1.ExternalWorkload, got %#v", oldObj)
			}

			updated, ok := newObj.(*ewv1alpha1.ExternalWorkload)
			if !ok {
				ec.log.Errorf("error processing ExternalWorkload event: expected *v1alpha1.ExternalWorkload, got %#v", newObj)
			}

			// Ignore resync updates. If nothing has changed in the object, then
			// the update processing is redudant.
			if old.ResourceVersion == updated.ResourceVersion {
				ec.log.Tracef("skipping ExternalWorkload resync update event")
				return
			}

			// Compute whether any labels have changed in between updates. If
			// they have, this might affect service membership so we need to
			// process the update.
			labelsChanged := false
			if len(old.Labels) != len(updated.Labels) {
				labelsChanged = true
			} else {
				for upK, upV := range updated.Labels {
					oldV, ok := old.Labels[upK]
					if ok && oldV == upV {
						// Labels match
						continue
					} else {
						labelsChanged = true
						break
					}
				}
			}

			readinessChanged := isReady(old) != isReady(updated)

			// Check if anything in the spec has changed in between versions.
			specChanged := false
			ports := make(map[int32]struct{})
			for _, pSpec := range updated.Spec.Ports {
				ports[pSpec.Port] = struct{}{}
			}
			for _, pSpec := range old.Spec.Ports {
				if _, ok := ports[pSpec.Port]; !ok {
					specChanged = true
					break
				}
			}

			ips := make(map[string]struct{})
			for _, addr := range updated.Spec.WorkloadIPs {
				ips[addr.Ip] = struct{}{}
			}
			for _, addr := range old.Spec.WorkloadIPs {
				if _, ok := ips[addr.Ip]; !ok {
					specChanged = true
					break
				}
			}

			// If nothing has changed, don't bother with the update
			if !readinessChanged && !specChanged && !labelsChanged {
				ec.log.Debugf("skipping ExternalWorkload update; nothing has changed between old rv %s and new rv %s", old.ResourceVersion, updated.ResourceVersion)
				return
			}

			services, err := ec.getSvcMembership(updated)
			if err != nil {
				ec.log.Errorf("failed to get service membership for %s/%s: %v", updated.Namespace, updated.Name, err)
				return
			}

			// If the labels have changed, membership for the services has
			// changed. We need to figure out which services to update.
			if labelsChanged {
				// Get a list of services for the old set.
				oldServices, err := ec.getSvcMembership(old)
				if err != nil {
					ec.log.Errorf("failed to get service membership for %s/%s: %v", old.Namespace, old.Name, err)
					return
				}

				// When the workload spec has changed in-between versions (or
				// the readiness) we will need to update old services (services
				// that referenced the previous version) and new services
				if specChanged || readinessChanged {
					for k := range oldServices {
						services[k] = struct{}{}
					}
				} else {
					// When the spec / readiness has not changed, we simply need
					// to re-compute memberships. Do not take into account
					// services in common (since they are unchanged) updated
					// only services that select the old or the new
					disjoint := make(map[string]struct{})
					for k, v := range services {
						if _, ok := oldServices[k]; !ok {
							disjoint[k] = v
						}
					}

					for k, v := range oldServices {
						if _, ok := services[k]; !ok {
							disjoint[k] = v
						}
					}

					services = disjoint
				}
			}

			for svc := range services {
				ec.enqueueUpdate(svc)
			}
		},
	})

	if err != nil {
		return nil, err
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
			OnStartedLeading: func(context.Context) {
				ec.Lock()
				defer ec.Unlock()
				ec.isLeader = true
			},
			OnStoppedLeading: func() {
				ec.Lock()
				defer ec.Unlock()
				ec.isLeader = false
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

// Start will run the endpoint manager's processing loop and leader elector.
//
// The function will spawn two background tasks; one to run the leader elector
// client that and one that will process updates applied by the informer
// callbacks.
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
				return
			default:
			}
		}
	}()

	// Start a background task to process updates. When a shutdown signal is
	// received over the manager's stop channel, it is propagated to the elector
	// through the context object to ensure the elector task does not leak.
	go func() {
		for {
			select {
			case update := <-ec.updates:
				elapsed := time.Since(update.enqueTime).Seconds()
				ec.queueLatency.Observe(elapsed)

				ec.processUpdate(update.item)
			case <-ec.stop:
				ec.log.Info("received shutdown signal")
				// Propagate shutdown to elector through the context.
				cancel()
				return
			}
		}
	}()
}

// enqueueUpdate will enqueue a service key to the updates buffer to be
// processed by the endpoint manager. When a write to the buffer blocks, the
// update is dropped.
func (ec *EndpointsController) enqueueUpdate(key string) {
	update := queueUpdate{
		item:      key,
		enqueTime: time.Now(),
	}
	select {
	case ec.updates <- update:
		// Update successfully enqueued.
		ec.queueUpdates.Inc()
	default:
		// Drop update
		ec.queueDrops.Inc()
		ec.log.Debugf("External endpoint manager queue is full; dropping update for %s", update.item)
		return
	}
}

func (ec *EndpointsController) processUpdate(update string) {
	// TODO (matei): reconciliation logic
	// TODO (matei): track how long an item takes to be processed
	ec.log.Infof("Received %s", update)
}

// getSvcMembership accepts a pointer to an external workload resource and
// returns a set of service keys (<namespace>/<name>). The set includes all
// services local to the workload's namespace that match the workload.
func (ec *EndpointsController) getSvcMembership(workload *ewv1alpha1.ExternalWorkload) (map[string]struct{}, error) {
	set := make(map[string]struct{})
	services, err := ec.k8sAPI.Svc().Lister().Services(workload.Namespace).List(labels.Everything())
	if err != nil {
		return set, err
	}

	for _, svc := range services {
		if svc.Spec.Selector == nil {
			continue
		}

		// Taken from official k8s code, this checks whether a given object has
		// a deleted state before returning a `namespace/name` key. This is
		// important since we do not want to consider a service that has been
		// deleted and is waiting for cache eviction
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(svc)
		if err != nil {
			return map[string]struct{}{}, err
		}

		// Check if service selects our ee.
		if labels.ValidatedSetSelector(svc.Spec.Selector).Matches(labels.Set(workload.Labels)) {
			set[key] = struct{}{}
		}
	}

	return set, nil
}

// === Callbacks ===

// When a service update has been received (regardless of the event type, i.e.
// can be Added, Modified, Deleted) send it to the endpoint controller for
// processing.
func (ec *EndpointsController) updateService(obj interface{}) {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		ec.log.Errorf("error processing Service event: expected *corev1.Service, got %#v", obj)
		return
	}

	if svc.Namespace == "kube-system" {
		ec.log.Tracef("skipping Service event: %s/%s is a kube-system Service", svc.Namespace, svc.Name)
		return
	}
	// Use client-go's generic key function to make a key of the format
	// <namespace>/<name>
	key, err := cache.MetaNamespaceKeyFunc(svc)
	if err != nil {
		ec.log.Infof("failed to get key for svc %s/%s: %v", svc.Namespace, svc.Name, err)
	}

	ec.enqueueUpdate(key)
}

func isReady(ew *ewv1alpha1.ExternalWorkload) bool {
	if len(ew.Status.Conditions) == 0 {
		return false
	}

	// Loop through the conditions and look at each condition in turn starting
	// from the top.
	for i := range ew.Status.Conditions {
		cond := ew.Status.Conditions[i]
		// Stop once we find a 'Ready' condition. We expect a resource to only
		// have one 'Ready' type condition.
		if cond.Type == ewv1alpha1.WorkloadReady && cond.Status == ewv1alpha1.ConditionTrue {
			return true
		}
	}

	return false
}
