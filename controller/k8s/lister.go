package k8s

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

// GetObjects returns a list of Kubernetes objects, given a namespace, type, and name.
// If namespace is an empty string, match objects in all namespaces.
// If name is an empty string, match all objects of the given type.
func (l *Lister) GetObjects(namespace, restype, name string) ([]runtime.Object, error) {
	switch restype {
	case k8s.Namespaces:
		return l.getNamespaces(name)
	case k8s.Deployments:
		return l.getDeployments(namespace, name)
	case k8s.Pods:
		return l.getPods(namespace, name)
	case k8s.ReplicationControllers:
		return l.getRCs(namespace, name)
	case k8s.Services:
		return l.getServices(namespace, name)
	default:
		// TODO: ReplicaSet
		return nil, status.Errorf(codes.Unimplemented, "unimplemented resource type: %s", restype)
	}
}

// GetPodsFor returns all running and pending Pods associated with a given
// Kubernetes object. Use includeFailed to also get failed Pods
func (l *Lister) GetPodsFor(obj runtime.Object, includeFailed bool) ([]*apiv1.Pod, error) {
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
		if isPendingOrRunning(pod) || (includeFailed && isFailed(pod)) {
			allPods = append(allPods, pod)
		}
	}

	return allPods, nil
}

func (l *Lister) getNamespaces(name string) ([]runtime.Object, error) {
	var err error
	var namespaces []*apiv1.Namespace

	if name == "" {
		namespaces, err = l.NS.List(labels.Everything())
	} else {
		var namespace *apiv1.Namespace
		namespace, err = l.NS.Get(name)
		namespaces = []*apiv1.Namespace{namespace}
	}

	if err != nil {
		return nil, err
	}

	objects := []runtime.Object{}
	for _, ns := range namespaces {
		objects = append(objects, ns)
	}

	return objects, nil
}

func (l *Lister) getDeployments(namespace, name string) ([]runtime.Object, error) {
	var err error
	var deploys []*appsv1beta2.Deployment

	if namespace == "" {
		deploys, err = l.Deploy.List(labels.Everything())
	} else if name == "" {
		deploys, err = l.Deploy.Deployments(namespace).List(labels.Everything())
	} else {
		var deploy *appsv1beta2.Deployment
		deploy, err = l.Deploy.Deployments(namespace).Get(name)
		deploys = []*appsv1beta2.Deployment{deploy}
	}

	if err != nil {
		return nil, err
	}

	objects := []runtime.Object{}
	for _, deploy := range deploys {
		objects = append(objects, deploy)
	}

	return objects, nil
}

func (l *Lister) getPods(namespace, name string) ([]runtime.Object, error) {
	var err error
	var pods []*apiv1.Pod

	if namespace == "" {
		pods, err = l.Pod.List(labels.Everything())
	} else if name == "" {
		pods, err = l.Pod.Pods(namespace).List(labels.Everything())
	} else {
		var pod *apiv1.Pod
		pod, err = l.Pod.Pods(namespace).Get(name)
		pods = []*apiv1.Pod{pod}
	}

	if err != nil {
		return nil, err
	}

	objects := []runtime.Object{}
	for _, pod := range pods {
		objects = append(objects, pod)
	}

	return objects, nil
}

func (l *Lister) getRCs(namespace, name string) ([]runtime.Object, error) {
	var err error
	var rcs []*apiv1.ReplicationController

	if namespace == "" {
		rcs, err = l.RC.List(labels.Everything())
	} else if name == "" {
		rcs, err = l.RC.ReplicationControllers(namespace).List(labels.Everything())
	} else {
		var rc *apiv1.ReplicationController
		rc, err = l.RC.ReplicationControllers(namespace).Get(name)
		rcs = []*apiv1.ReplicationController{rc}
	}

	if err != nil {
		return nil, err
	}

	objects := []runtime.Object{}
	for _, rc := range rcs {
		objects = append(objects, rc)
	}

	return objects, nil
}

func (l *Lister) getServices(namespace, name string) ([]runtime.Object, error) {
	var err error
	var services []*apiv1.Service

	if namespace == "" {
		services, err = l.Svc.List(labels.Everything())
	} else if name == "" {
		services, err = l.Svc.Services(namespace).List(labels.Everything())
	} else {
		var svc *apiv1.Service
		svc, err = l.Svc.Services(namespace).Get(name)
		services = []*apiv1.Service{svc}
	}

	if err != nil {
		return nil, err
	}

	objects := []runtime.Object{}
	for _, svc := range services {
		objects = append(objects, svc)
	}

	return objects, nil
}

func isPendingOrRunning(pod *apiv1.Pod) bool {
	pending := pod.Status.Phase == apiv1.PodPending
	running := pod.Status.Phase == apiv1.PodRunning
	terminating := pod.DeletionTimestamp != nil
	return (pending || running) && !terminating
}

func isFailed(pod *apiv1.Pod) bool {
	return pod.Status.Phase == apiv1.PodFailed
}
