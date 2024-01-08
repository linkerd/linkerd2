package destination

import (
	"context"
	"fmt"
	"sync"
	"time"

	ewv1alpha1 "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1alpha1"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

const (
	// Value used in `kubernetes.io/managed-by` annotation
	managedByController = "linkerd-destination"

	// Specifies capacity for updates buffer
	epUpdateQueueCapacity = 100

	// Name of the lease resource the controllem will use
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

// EndpointManagem
type EndpointManager struct {
	k8sAPI   *k8s.API
	log      *logging.Entry
	updates  chan string
	stop     chan struct{}
	isLeader bool

	lec leaderelection.LeaderElectionConfig
	sync.RWMutex
}

func NewEndpointManager(k8sAPI *k8s.API, stopCh chan struct{}, hostname, controllerNs string) (*EndpointManager, error) {
	em := &EndpointManager{
		k8sAPI:  k8sAPI,
		updates: make(chan string, epUpdateQueueCapacity),
		stop:    stopCh,
		log: logging.WithFields(logging.Fields{
			"component": "external-endpoint-manager",
		}),
	}

	_, err := k8sAPI.Svc().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    em.updateService,
		DeleteFunc: em.updateService,
		UpdateFunc: func(_, newObj interface{}) {
			em.updateService(newObj)
		},
	})

	if err != nil {
		return nil, err
	}

	_, err = k8sAPI.ExtWorkload().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: em.updateExternal,
		DeleteFunc: func(obj interface{}) {
			var ew *ewv1alpha1.ExternalWorkload
			if ew, ok := obj.(*ewv1alpha1.ExternalWorkload); ok {
				em.updateExternal(ew)
				return
			}

			tomb, ok := obj.(cache.DeletedFinalStateUnknown)
			if !ok {
				return
			}

			ew, ok = tomb.Obj.(*ewv1alpha1.ExternalWorkload)
			if !ok {
				return
			}

			em.updateExternal(ew)
		},
		UpdateFunc: func(_, newObj interface{}) {
			em.updateExternal(newObj)
		},
	})

	if err != nil {
		return nil, err
	}

	// Store configuration for leader elector client. The leader elector will
	// accept three callbacks. When a lease is claimed, the elector will mark
	// the manager as a 'leader'. When a lease is released, the elector will set
	// the isLeader value back to false.
	em.lec = leaderelection.LeaderElectionConfig{
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
				em.Lock()
				defer em.Unlock()
				em.isLeader = true
			},
			OnStoppedLeading: func() {
				em.Lock()
				defer em.Unlock()
				em.isLeader = false
				em.log.Infof("%s released lease", hostname)
			},
			OnNewLeader: func(identity string) {
				if identity == hostname {
					em.log.Infof("%s acquired lease", hostname)
				}
			},
		},
	}

	return em, nil
}

// Start will run the endpoint manager's processing loop and leader elector.
//
// The function will spawn two background tasks; one to run the leader elector
// client that and one that will process updates applied by the informer
// callbacks.
func (em *EndpointManager) Start() {
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
			leaderelection.RunOrDie(ctx, em.lec)
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
			case update := <-em.updates:
				em.processUpdate(update)
			case <-em.stop:
				em.log.Info("Received shutdown signal")
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
func (em *EndpointManager) enqueueUpdate(update string) {
	select {
	case em.updates <- update:
		// Update successfully enqueued.
	default:
		// Drop update
		// TODO (matei): add overflow counter
		em.log.Debugf("External endpoint manager queue is full; dropping update for %s", update)
		return
	}
}

func (em *EndpointManager) processUpdate(update string) {
	// TODO (matei): reconciliation logic
	em.log.Infof("Received %s", update)
}

// getSvcMembership accepts a pointer to an external workload resource and
// returns a set of service keys (<namespace>/<name>). The set includes all
// services local to the workload's namespace that match the workload.
func (em *EndpointManager) getSvcMembership(workload *ewv1alpha1.ExternalWorkload) (map[string]struct{}, error) {
	set := make(map[string]struct{})
	services, err := em.k8sAPI.Svc().Lister().Services(workload.Namespace).List(labels.Everything())
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
// can be Added, Modified, Deleted) send it to the endpoint manager for
// processing.
func (em *EndpointManager) updateService(obj interface{}) {
	// Use client-go's generic key function to make a key of the format
	// <namespace>/<name>
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		em.log.Infof("Failed to get key for obj %v: %v", obj, err)
	}

	em.enqueueUpdate(key)
}

func (em *EndpointManager) updateExternal(obj interface{}) {
	ew := obj.(*ewv1alpha1.ExternalWorkload)
	services, err := em.getSvcMembership(ew)
	if err != nil {
		em.log.Errorf("Failed to get service membership for %s/%s: %v", ew.Namespace, ew.Name, err)
		return
	}

	for svc := range services {
		fmt.Printf("Hello from %s\n", ew.Name)
		em.enqueueUpdate(svc)
	}
}

func isReady(workload *ewv1alpha1.ExternalWorkload) bool {
	if workload.Status.Conditions == nil || len(workload.Status.Conditions) == 0 {
		return false
	}

	var cond *ewv1alpha1.WorkloadCondition
	for i := range workload.Status.Conditions {
		if workload.Status.Conditions[i].Type == ewv1alpha1.WorkloadReady {
			cond = &workload.Status.Conditions[i]
		}
	}

	return cond.Status == ewv1alpha1.ConditionTrue
}
