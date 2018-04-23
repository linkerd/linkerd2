package k8s

import (
	"context"
	"errors"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	appsv1beta2 "k8s.io/api/apps/v1beta2"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	applisters "k8s.io/client-go/listers/apps/v1beta2"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

// Lister wraps client-go Lister types for all Kubernetes objects
type Lister struct {
	NS     corelisters.NamespaceLister
	Deploy applisters.DeploymentLister
	RS     applisters.ReplicaSetLister
	Pod    corelisters.PodLister
	RC     corelisters.ReplicationControllerLister
	Svc    corelisters.ServiceLister

	nsSynced     cache.InformerSynced
	deploySynced cache.InformerSynced
	rsSynced     cache.InformerSynced
	podSynced    cache.InformerSynced
	rcSynced     cache.InformerSynced
	svcSynced    cache.InformerSynced
}

// NewLister takes a Kubernetes client and returns an initialized Lister
func NewLister(k8sClient kubernetes.Interface) *Lister {
	sharedInformers := informers.NewSharedInformerFactory(k8sClient, 10*time.Minute)

	namespaceInformer := sharedInformers.Core().V1().Namespaces()
	deployInformer := sharedInformers.Apps().V1beta2().Deployments()
	replicaSetInformer := sharedInformers.Apps().V1beta2().ReplicaSets()
	podInformer := sharedInformers.Core().V1().Pods()
	replicationControllerInformer := sharedInformers.Core().V1().ReplicationControllers()
	serviceInformer := sharedInformers.Core().V1().Services()

	lister := &Lister{
		NS:     namespaceInformer.Lister(),
		Deploy: deployInformer.Lister(),
		RS:     replicaSetInformer.Lister(),
		Pod:    podInformer.Lister(),
		RC:     replicationControllerInformer.Lister(),
		Svc:    serviceInformer.Lister(),

		nsSynced:     namespaceInformer.Informer().HasSynced,
		deploySynced: deployInformer.Informer().HasSynced,
		rsSynced:     replicaSetInformer.Informer().HasSynced,
		podSynced:    podInformer.Informer().HasSynced,
		rcSynced:     replicationControllerInformer.Informer().HasSynced,
		svcSynced:    serviceInformer.Informer().HasSynced,
	}

	// this must be called after the Lister() methods
	sharedInformers.Start(nil)

	return lister
}

// Sync waits for all informers to be synced.
// For servers, call this asynchronously.
// For testing, call this synchronously.
func (l *Lister) Sync() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log.Infof("waiting for caches to sync")
	if !cache.WaitForCacheSync(
		ctx.Done(),
		l.nsSynced,
		l.deploySynced,
		l.rsSynced,
		l.podSynced,
		l.rcSynced,
		l.svcSynced,
	) {
		return errors.New("timed out wait for caches to sync")
	}
	log.Infof("caches synced")

	return nil
}

// GetPodsFor returns all running and pending Pods associated with a given
// Kubernetes object.
func (l *Lister) GetPodsFor(obj runtime.Object) ([]*apiv1.Pod, error) {
	var namespace string
	var selector labels.Selector

	switch typed := obj.(type) {
	case *apiv1.Namespace:
		namespace = typed.Name
		selector = labels.Everything()

	case *appsv1beta2.Deployment:
		namespace = typed.Namespace
		selector = labels.Set(typed.Spec.Selector.MatchLabels).AsSelector()

	case *appsv1beta2.ReplicaSet:
		namespace = typed.Namespace
		selector = labels.Set(typed.Spec.Selector.MatchLabels).AsSelector()

	case *apiv1.ReplicationController:
		namespace = typed.Namespace
		selector = labels.Set(typed.Spec.Selector).AsSelector()

	case *apiv1.Service:
		namespace = typed.Namespace
		selector = labels.Set(typed.Spec.Selector).AsSelector()

	case *apiv1.Pod:
		namespace = typed.Namespace
		selector = labels.Everything()

	default:
		return nil, fmt.Errorf("Cannot get object selector: %v", obj)
	}

	pods, err := l.Pod.Pods(namespace).List(selector)
	if err != nil {
		return nil, err
	}

	allPods := []*apiv1.Pod{}
	for _, pod := range pods {
		if isPendingOrRunning(pod) {
			allPods = append(allPods, pod)
		}
	}

	return allPods, nil
}

func isPendingOrRunning(pod *apiv1.Pod) bool {
	pending := pod.Status.Phase == apiv1.PodPending
	running := pod.Status.Phase == apiv1.PodRunning
	terminating := pod.DeletionTimestamp != nil
	return (pending || running) && !terminating
}
