package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	tsclient "github.com/deislabs/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	ts "github.com/deislabs/smi-sdk-go/pkg/gen/client/split/informers/externalversions"
	tsinformers "github.com/deislabs/smi-sdk-go/pkg/gen/client/split/informers/externalversions/split/v1alpha1"
	spv1alpha2 "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	spclient "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	sp "github.com/linkerd/linkerd2/controller/gen/client/informers/externalversions"
	spinformers "github.com/linkerd/linkerd2/controller/gen/client/informers/externalversions/serviceprofile/v1alpha2"
	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	arinformers "k8s.io/client-go/informers/admissionregistration/v1beta1"
	appv1informers "k8s.io/client-go/informers/apps/v1"
	batchv1informers "k8s.io/client-go/informers/batch/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// APIResource is an enum for Kubernetes API resource types, for use when
// initializing a K8s API, to describe which resource types to interact with.
type APIResource int

// These constants enumerate Kubernetes resource types.
const (
	CM APIResource = iota
	Deploy
	DS
	Endpoint
	Job
	MWC // mutating webhook configuration
	NS
	Pod
	RC
	RS
	SP
	SS
	Svc
	TS
	Node
)

// API provides shared informers for all Kubernetes objects
type API struct {
	Client kubernetes.Interface

	cm       coreinformers.ConfigMapInformer
	deploy   appv1informers.DeploymentInformer
	ds       appv1informers.DaemonSetInformer
	endpoint coreinformers.EndpointsInformer
	job      batchv1informers.JobInformer
	mwc      arinformers.MutatingWebhookConfigurationInformer
	ns       coreinformers.NamespaceInformer
	pod      coreinformers.PodInformer
	rc       coreinformers.ReplicationControllerInformer
	rs       appv1informers.ReplicaSetInformer
	sp       spinformers.ServiceProfileInformer
	ss       appv1informers.StatefulSetInformer
	svc      coreinformers.ServiceInformer
	ts       tsinformers.TrafficSplitInformer
	node     coreinformers.NodeInformer

	syncChecks        []cache.InformerSynced
	sharedInformers   informers.SharedInformerFactory
	spSharedInformers sp.SharedInformerFactory
	tsSharedInformers ts.SharedInformerFactory
}

// InitializeAPI creates Kubernetes clients and returns an initialized API wrapper.
func InitializeAPI(kubeConfig string, resources ...APIResource) (*API, error) {
	k8sClient, err := k8s.NewAPI(kubeConfig, "", "", 0)
	if err != nil {
		return nil, err
	}

	// check for cluster-wide access
	err = k8s.ClusterAccess(k8sClient)
	if err != nil {
		return nil, err
	}

	// check for need and access to ServiceProfiles
	var spClient *spclient.Clientset
	for _, res := range resources {
		if res == SP {
			err := k8s.ServiceProfilesAccess(k8sClient)
			if err != nil {
				return nil, err
			}

			spClient, err = NewSpClientSet(kubeConfig)
			if err != nil {
				return nil, err
			}

			break
		}
	}

	// TrafficSplits
	var tsClient *tsclient.Clientset
	for _, res := range resources {
		if res == TS {
			tsClient, err = NewTsClientSet(kubeConfig)
			if err != nil {
				return nil, err
			}

			break
		}
	}
	return NewAPI(k8sClient, spClient, tsClient, resources...), nil
}

// NewAPI takes a Kubernetes client and returns an initialized API.
func NewAPI(
	k8sClient kubernetes.Interface,
	spClient spclient.Interface,
	tsClient tsclient.Interface,
	resources ...APIResource,
) *API {
	sharedInformers := informers.NewSharedInformerFactory(k8sClient, 10*time.Minute)

	var spSharedInformers sp.SharedInformerFactory
	if spClient != nil {
		spSharedInformers = sp.NewSharedInformerFactory(spClient, 10*time.Minute)
	}

	var tsSharedInformers ts.SharedInformerFactory
	if tsClient != nil {
		tsSharedInformers = ts.NewSharedInformerFactory(tsClient, 10*time.Minute)
	}

	api := &API{
		Client:            k8sClient,
		syncChecks:        make([]cache.InformerSynced, 0),
		sharedInformers:   sharedInformers,
		spSharedInformers: spSharedInformers,
		tsSharedInformers: tsSharedInformers,
	}

	for _, resource := range resources {
		switch resource {
		case CM:
			api.cm = sharedInformers.Core().V1().ConfigMaps()
			api.syncChecks = append(api.syncChecks, api.cm.Informer().HasSynced)
		case Deploy:
			api.deploy = sharedInformers.Apps().V1().Deployments()
			api.syncChecks = append(api.syncChecks, api.deploy.Informer().HasSynced)
		case DS:
			api.ds = sharedInformers.Apps().V1().DaemonSets()
			api.syncChecks = append(api.syncChecks, api.ds.Informer().HasSynced)
		case Endpoint:
			api.endpoint = sharedInformers.Core().V1().Endpoints()
			api.syncChecks = append(api.syncChecks, api.endpoint.Informer().HasSynced)
		case Job:
			api.job = sharedInformers.Batch().V1().Jobs()
			api.syncChecks = append(api.syncChecks, api.job.Informer().HasSynced)
		case MWC:
			api.mwc = sharedInformers.Admissionregistration().V1beta1().MutatingWebhookConfigurations()
			api.syncChecks = append(api.syncChecks, api.mwc.Informer().HasSynced)
		case NS:
			api.ns = sharedInformers.Core().V1().Namespaces()
			api.syncChecks = append(api.syncChecks, api.ns.Informer().HasSynced)
		case Pod:
			api.pod = sharedInformers.Core().V1().Pods()
			api.syncChecks = append(api.syncChecks, api.pod.Informer().HasSynced)
		case RC:
			api.rc = sharedInformers.Core().V1().ReplicationControllers()
			api.syncChecks = append(api.syncChecks, api.rc.Informer().HasSynced)
		case RS:
			api.rs = sharedInformers.Apps().V1().ReplicaSets()
			api.syncChecks = append(api.syncChecks, api.rs.Informer().HasSynced)
		case SP:
			api.sp = spSharedInformers.Linkerd().V1alpha2().ServiceProfiles()
			api.syncChecks = append(api.syncChecks, api.sp.Informer().HasSynced)
		case SS:
			api.ss = sharedInformers.Apps().V1().StatefulSets()
			api.syncChecks = append(api.syncChecks, api.ss.Informer().HasSynced)
		case Svc:
			api.svc = sharedInformers.Core().V1().Services()
			api.syncChecks = append(api.syncChecks, api.svc.Informer().HasSynced)
		case TS:
			api.ts = tsSharedInformers.Split().V1alpha1().TrafficSplits()
			api.syncChecks = append(api.syncChecks, api.ts.Informer().HasSynced)
		case Node:
			api.node = sharedInformers.Core().V1().Nodes()
			api.syncChecks = append(api.syncChecks, api.node.Informer().HasSynced)
		}
	}

	return api
}

// Sync waits for all informers to be synced.
func (api *API) Sync() {
	api.sharedInformers.Start(nil)
	api.spSharedInformers.Start(nil)
	api.tsSharedInformers.Start(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log.Infof("waiting for caches to sync")
	if !cache.WaitForCacheSync(ctx.Done(), api.syncChecks...) {
		log.Fatal("failed to sync caches")
	}
	log.Infof("caches synced")
}

// NS provides access to a shared informer and lister for Namespaces.
func (api *API) NS() coreinformers.NamespaceInformer {
	if api.ns == nil {
		panic("NS informer not configured")
	}
	return api.ns
}

// Deploy provides access to a shared informer and lister for Deployments.
func (api *API) Deploy() appv1informers.DeploymentInformer {
	if api.deploy == nil {
		panic("Deploy informer not configured")
	}
	return api.deploy
}

// DS provides access to a shared informer and lister for Daemonsets.
func (api *API) DS() appv1informers.DaemonSetInformer {
	if api.ds == nil {
		panic("DS informer not configured")
	}
	return api.ds
}

// SS provides access to a shared informer and lister for Statefulsets.
func (api *API) SS() appv1informers.StatefulSetInformer {
	if api.ss == nil {
		panic("SS informer not configured")
	}
	return api.ss
}

// RS provides access to a shared informer and lister for ReplicaSets.
func (api *API) RS() appv1informers.ReplicaSetInformer {
	if api.rs == nil {
		panic("RS informer not configured")
	}
	return api.rs
}

// Pod provides access to a shared informer and lister for Pods.
func (api *API) Pod() coreinformers.PodInformer {
	if api.pod == nil {
		panic("Pod informer not configured")
	}
	return api.pod
}

// RC provides access to a shared informer and lister for
// ReplicationControllers.
func (api *API) RC() coreinformers.ReplicationControllerInformer {
	if api.rc == nil {
		panic("RC informer not configured")
	}
	return api.rc
}

// Svc provides access to a shared informer and lister for Services.
func (api *API) Svc() coreinformers.ServiceInformer {
	if api.svc == nil {
		panic("Svc informer not configured")
	}
	return api.svc
}

// Endpoint provides access to a shared informer and lister for Endpoints.
func (api *API) Endpoint() coreinformers.EndpointsInformer {
	if api.endpoint == nil {
		panic("Endpoint informer not configured")
	}
	return api.endpoint
}

// CM provides access to a shared informer and lister for ConfigMaps.
func (api *API) CM() coreinformers.ConfigMapInformer {
	if api.cm == nil {
		panic("CM informer not configured")
	}
	return api.cm
}

// SP provides access to a shared informer and lister for ServiceProfiles.
func (api *API) SP() spinformers.ServiceProfileInformer {
	if api.sp == nil {
		panic("SP informer not configured")
	}
	return api.sp
}

// MWC provides access to a shared informer and lister for MutatingWebhookConfigurations.
func (api *API) MWC() arinformers.MutatingWebhookConfigurationInformer {
	if api.mwc == nil {
		panic("MWC informer not configured")
	}
	return api.mwc
}

//Job provides access to a shared informer and lister for Jobs.
func (api *API) Job() batchv1informers.JobInformer {
	if api.job == nil {
		panic("Job informer not configured")
	}
	return api.job
}

// SPAvailable informs the caller whether this API is configured to retrieve
// ServiceProfiles
func (api *API) SPAvailable() bool {
	return api.sp != nil
}

// TS provides access to a shared informer and lister for TrafficSplits.
func (api *API) TS() tsinformers.TrafficSplitInformer {
	if api.ts == nil {
		panic("TS informer not configured")
	}
	return api.ts
}

// Node provides access to a shared informer and lister for Nodes.
func (api *API) Node() coreinformers.NodeInformer {
	if api.node == nil {
		panic("Node informer not configured")
	}
	return api.node
}

// GetObjects returns a list of Kubernetes objects, given a namespace, type, and name.
// If namespace is an empty string, match objects in all namespaces.
// If name is an empty string, match all objects of the given type.
func (api *API) GetObjects(namespace, restype, name string) ([]runtime.Object, error) {
	switch restype {
	case k8s.Namespace:
		return api.getNamespaces(name)
	case k8s.DaemonSet:
		return api.getDaemonsets(namespace, name)
	case k8s.Deployment:
		return api.getDeployments(namespace, name)
	case k8s.Job:
		return api.getJobs(namespace, name)
	case k8s.Pod:
		return api.getPods(namespace, name)
	case k8s.ReplicationController:
		return api.getRCs(namespace, name)
	case k8s.Service:
		return api.getServices(namespace, name)
	case k8s.StatefulSet:
		return api.getStatefulsets(namespace, name)
	default:
		// TODO: ReplicaSet
		return nil, status.Errorf(codes.Unimplemented, "unimplemented resource type: %s", restype)
	}
}

// GetOwnerKindAndName returns the pod owner's kind and name, using owner
// references from the Kubernetes API. The kind is represented as the Kubernetes
// singular resource type (e.g. deployment, daemonset, job, etc.).
// If retry is true, when the shared informer cache doesn't return anything
// we try again with a direct Kubernetes API call.
func (api *API) GetOwnerKindAndName(pod *corev1.Pod, retry bool) (string, string) {
	ownerRefs := pod.GetOwnerReferences()
	if len(ownerRefs) == 0 {
		// pod without a parent
		return "pod", pod.Name
	} else if len(ownerRefs) > 1 {
		log.Debugf("unexpected owner reference count (%d): %+v", len(ownerRefs), ownerRefs)
		return "pod", pod.Name
	}

	parent := ownerRefs[0]
	if parent.Kind == "ReplicaSet" {
		var rs *appsv1.ReplicaSet
		var err error
		rs, err = api.RS().Lister().ReplicaSets(pod.Namespace).Get(parent.Name)
		if err != nil {
			log.Warnf("failed to retrieve replicaset from indexer %s/%s: %s", pod.Namespace, parent.Name, err)
			if retry {
				rs, err = api.Client.AppsV1().ReplicaSets(pod.Namespace).Get(parent.Name, metav1.GetOptions{})
				if err != nil {
					log.Warnf("failed to retrieve replicaset from direct API call %s/%s: %s", pod.Namespace, parent.Name, err)
				}
			}
		}

		if err != nil || len(rs.GetOwnerReferences()) != 1 {
			return strings.ToLower(parent.Kind), parent.Name
		}
		rsParent := rs.GetOwnerReferences()[0]
		return strings.ToLower(rsParent.Kind), rsParent.Name
	}

	return strings.ToLower(parent.Kind), parent.Name
}

// GetPodsFor returns all running and pending Pods associated with a given
// Kubernetes object. Use includeFailed to also get failed Pods
func (api *API) GetPodsFor(obj runtime.Object, includeFailed bool) ([]*corev1.Pod, error) {
	var namespace string
	var selector labels.Selector
	var ownerUID types.UID
	var err error

	pods := []*corev1.Pod{}
	switch typed := obj.(type) {
	case *corev1.Namespace:
		namespace = typed.Name
		selector = labels.Everything()

	case *appsv1.DaemonSet:
		namespace = typed.Namespace
		selector = labels.Set(typed.Spec.Selector.MatchLabels).AsSelector()
		ownerUID = typed.UID

	case *appsv1.Deployment:
		namespace = typed.Namespace
		selector = labels.Set(typed.Spec.Selector.MatchLabels).AsSelector()
		ret, err := api.RS().Lister().ReplicaSets(namespace).List(selector)
		if err != nil {
			return nil, err
		}
		for _, rs := range ret {
			if isOwner(typed.UID, rs.GetOwnerReferences()) {
				podsRS, err := api.GetPodsFor(rs, includeFailed)
				if err != nil {
					return nil, err
				}
				pods = append(pods, podsRS...)
			}
		}
		return pods, nil

	case *appsv1.ReplicaSet:
		namespace = typed.Namespace
		selector = labels.Set(typed.Spec.Selector.MatchLabels).AsSelector()
		ownerUID = typed.UID

	case *batchv1.Job:
		namespace = typed.Namespace
		selector = labels.Set(typed.Spec.Selector.MatchLabels).AsSelector()
		ownerUID = typed.UID

	case *corev1.ReplicationController:
		namespace = typed.Namespace
		selector = labels.Set(typed.Spec.Selector).AsSelector()
		ownerUID = typed.UID

	case *corev1.Service:
		if typed.Spec.Type == corev1.ServiceTypeExternalName {
			return []*corev1.Pod{}, nil
		}
		namespace = typed.Namespace
		selector = labels.Set(typed.Spec.Selector).AsSelector()

	case *appsv1.StatefulSet:
		namespace = typed.Namespace
		selector = labels.Set(typed.Spec.Selector.MatchLabels).AsSelector()
		ownerUID = typed.UID

	case *corev1.Pod:
		// Special case for pods:
		// GetPodsFor a pod should just return the pod itself
		namespace = typed.Namespace
		pod, err := api.Pod().Lister().Pods(typed.Namespace).Get(typed.Name)
		if err != nil {
			return nil, err
		}
		pods = []*corev1.Pod{pod}

	default:
		return nil, fmt.Errorf("Cannot get object selector: %v", obj)
	}

	// if obj.(type) is Pod, we've already retrieved it and put it in pods
	// for the other types, pods will still be empty
	if len(pods) == 0 {
		pods, err = api.Pod().Lister().Pods(namespace).List(selector)
		if err != nil {
			return nil, err
		}
	}

	allPods := []*corev1.Pod{}
	for _, pod := range pods {
		if isPendingOrRunning(pod) || (includeFailed && isFailed(pod)) {
			if ownerUID == "" {
				allPods = append(allPods, pod)
			} else if isOwner(ownerUID, pod.GetOwnerReferences()) {
				allPods = append(allPods, pod)
			}
		}
	}
	return allPods, nil
}

func isOwner(u types.UID, ownerRefs []metav1.OwnerReference) bool {
	for _, or := range ownerRefs {
		if u == or.UID {
			return true
		}
	}
	return false
}

// GetNameAndNamespaceOf returns the name and namespace of the given object.
func GetNameAndNamespaceOf(obj runtime.Object) (string, string, error) {
	switch typed := obj.(type) {
	case *corev1.Namespace:
		return typed.Name, typed.Name, nil

	case *appsv1.DaemonSet:
		return typed.Name, typed.Namespace, nil

	case *appsv1.Deployment:
		return typed.Name, typed.Namespace, nil

	case *batchv1.Job:
		return typed.Name, typed.Namespace, nil

	case *appsv1.ReplicaSet:
		return typed.Name, typed.Namespace, nil

	case *corev1.ReplicationController:
		return typed.Name, typed.Namespace, nil

	case *corev1.Service:
		return typed.Name, typed.Namespace, nil

	case *appsv1.StatefulSet:
		return typed.Name, typed.Namespace, nil

	case *corev1.Pod:
		return typed.Name, typed.Namespace, nil

	default:
		return "", "", fmt.Errorf("Cannot determine object type: %v", obj)
	}
}

// GetNameOf returns the name of the given object.
func GetNameOf(obj runtime.Object) (string, error) {
	name, _, err := GetNameAndNamespaceOf(obj)
	if err != nil {
		return "", err
	}
	return name, nil
}

// GetNamespaceOf returns the namespace of the given object.
func GetNamespaceOf(obj runtime.Object) (string, error) {
	_, namespace, err := GetNameAndNamespaceOf(obj)
	if err != nil {
		return "", err
	}
	return namespace, nil
}

// getNamespaces returns the namespace matching the specified name. If no name
// is given, it returns all namespaces.
func (api *API) getNamespaces(name string) ([]runtime.Object, error) {
	var namespaces []*corev1.Namespace

	if name == "" {
		var err error
		namespaces, err = api.NS().Lister().List(labels.Everything())
		if err != nil {
			return nil, err
		}
	} else {
		namespace, err := api.NS().Lister().Get(name)
		if err != nil {
			return nil, err
		}
		namespaces = []*corev1.Namespace{namespace}
	}

	objects := []runtime.Object{}
	for _, ns := range namespaces {
		objects = append(objects, ns)
	}

	return objects, nil
}

func (api *API) getDeployments(namespace, name string) ([]runtime.Object, error) {
	var err error
	var deploys []*appsv1.Deployment

	if namespace == "" {
		deploys, err = api.Deploy().Lister().List(labels.Everything())
	} else if name == "" {
		deploys, err = api.Deploy().Lister().Deployments(namespace).List(labels.Everything())
	} else {
		var deploy *appsv1.Deployment
		deploy, err = api.Deploy().Lister().Deployments(namespace).Get(name)
		deploys = []*appsv1.Deployment{deploy}
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

func (api *API) getPods(namespace, name string) ([]runtime.Object, error) {
	var err error
	var pods []*corev1.Pod

	if namespace == "" {
		pods, err = api.Pod().Lister().List(labels.Everything())
	} else if name == "" {
		pods, err = api.Pod().Lister().Pods(namespace).List(labels.Everything())
	} else {
		var pod *corev1.Pod
		pod, err = api.Pod().Lister().Pods(namespace).Get(name)
		pods = []*corev1.Pod{pod}
	}

	if err != nil {
		return nil, err
	}

	objects := []runtime.Object{}
	for _, pod := range pods {
		if !isPendingOrRunning(pod) {
			continue
		}
		objects = append(objects, pod)
	}

	return objects, nil
}

func (api *API) getRCs(namespace, name string) ([]runtime.Object, error) {
	var err error
	var rcs []*corev1.ReplicationController

	if namespace == "" {
		rcs, err = api.RC().Lister().List(labels.Everything())
	} else if name == "" {
		rcs, err = api.RC().Lister().ReplicationControllers(namespace).List(labels.Everything())
	} else {
		var rc *corev1.ReplicationController
		rc, err = api.RC().Lister().ReplicationControllers(namespace).Get(name)
		rcs = []*corev1.ReplicationController{rc}
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

func (api *API) getDaemonsets(namespace, name string) ([]runtime.Object, error) {
	var err error
	var daemonsets []*appsv1.DaemonSet

	if namespace == "" {
		daemonsets, err = api.DS().Lister().List(labels.Everything())
	} else if name == "" {
		daemonsets, err = api.DS().Lister().DaemonSets(namespace).List(labels.Everything())
	} else {
		var ds *appsv1.DaemonSet
		ds, err = api.DS().Lister().DaemonSets(namespace).Get(name)
		daemonsets = []*appsv1.DaemonSet{ds}
	}

	if err != nil {
		return nil, err
	}

	objects := []runtime.Object{}
	for _, ds := range daemonsets {
		objects = append(objects, ds)
	}

	return objects, nil
}

func (api *API) getStatefulsets(namespace, name string) ([]runtime.Object, error) {
	var err error
	var statefulsets []*appsv1.StatefulSet

	if namespace == "" {
		statefulsets, err = api.SS().Lister().List(labels.Everything())
	} else if name == "" {
		statefulsets, err = api.SS().Lister().StatefulSets(namespace).List(labels.Everything())
	} else {
		var ss *appsv1.StatefulSet
		ss, err = api.SS().Lister().StatefulSets(namespace).Get(name)
		statefulsets = []*appsv1.StatefulSet{ss}
	}

	if err != nil {
		return nil, err
	}

	objects := []runtime.Object{}
	for _, ss := range statefulsets {
		objects = append(objects, ss)
	}

	return objects, nil
}

func (api *API) getJobs(namespace, name string) ([]runtime.Object, error) {
	var err error
	var jobs []*batchv1.Job

	if namespace == "" {
		jobs, err = api.Job().Lister().List(labels.Everything())
	} else if name == "" {
		jobs, err = api.Job().Lister().Jobs(namespace).List(labels.Everything())
	} else {
		var job *batchv1.Job
		job, err = api.Job().Lister().Jobs(namespace).Get(name)
		jobs = []*batchv1.Job{job}
	}

	if err != nil {
		return nil, err
	}

	objects := []runtime.Object{}
	for _, job := range jobs {
		objects = append(objects, job)
	}

	return objects, nil
}

func (api *API) getServices(namespace, name string) ([]runtime.Object, error) {
	services, err := api.GetServices(namespace, name)

	if err != nil {
		return nil, err
	}

	objects := []runtime.Object{}
	for _, svc := range services {
		objects = append(objects, svc)
	}

	return objects, nil
}

// GetServices returns a list of Service resources, based on input namespace and
// name.
func (api *API) GetServices(namespace, name string) ([]*corev1.Service, error) {
	var err error
	var services []*corev1.Service

	if namespace == "" {
		services, err = api.Svc().Lister().List(labels.Everything())
	} else if name == "" {
		services, err = api.Svc().Lister().Services(namespace).List(labels.Everything())
	} else {
		var svc *corev1.Service
		svc, err = api.Svc().Lister().Services(namespace).Get(name)
		services = []*corev1.Service{svc}
	}

	return services, err
}

// GetServicesFor returns all Service resources which include a pod of the given
// resource object.  In other words, it returns all Services of which the given
// resource object is a part of.
func (api *API) GetServicesFor(obj runtime.Object, includeFailed bool) ([]*corev1.Service, error) {
	if svc, ok := obj.(*corev1.Service); ok {
		return []*corev1.Service{svc}, nil
	}

	pods, err := api.GetPodsFor(obj, includeFailed)
	if err != nil {
		return nil, err
	}
	namespace, err := GetNamespaceOf(obj)
	if err != nil {
		return nil, err
	}
	allServices, err := api.GetServices(namespace, "")
	if err != nil {
		return nil, err
	}
	services := make([]*corev1.Service, 0)
	for _, svc := range allServices {
		leaves, err := api.getLeafServices(svc)
		if err != nil {
			return nil, err
		}
		svcPods := []*corev1.Pod{}
		if len(leaves) > 0 {
			for _, leaf := range leaves {
				pods, err := api.GetPodsFor(leaf, includeFailed)
				if err != nil {
					return nil, err
				}
				svcPods = append(svcPods, pods...)
			}
		} else {
			svcPods, err = api.GetPodsFor(svc, includeFailed)
			if err != nil {
				return nil, err
			}
		}

		if hasOverlap(pods, svcPods) {
			services = append(services, svc)
		}
	}
	return services, nil
}

func (api *API) getLeafServices(apex *corev1.Service) ([]*corev1.Service, error) {
	splits, err := api.TS().Lister().TrafficSplits(apex.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	leaves := []*corev1.Service{}
	for _, split := range splits {
		if split.Spec.Service == apex.Name {
			for _, backend := range split.Spec.Backends {
				if backend.Weight.Sign() == 1 {
					svc, err := api.Svc().Lister().Services(apex.Namespace).Get(backend.Service)
					if err != nil {
						log.Errorf("TrafficSplit %s/%s references non-existent service %s", apex.Namespace, split.Name, backend.Service)
						continue
					}
					leaves = append(leaves, svc)
				}
			}
		}
	}
	return leaves, nil
}

// GetServiceProfileFor returns the service profile for a given service.  We
// first look for a matching service profile in the client's namespace.  If not
// found, we then look in the service's namespace.  If no service profile is
// found, we return the default service profile.
func (api *API) GetServiceProfileFor(svc *corev1.Service, clientNs, clusterDomain string) *spv1alpha2.ServiceProfile {
	dst := fmt.Sprintf("%s.%s.svc.%s", svc.Name, svc.Namespace, clusterDomain)
	// First attempt to lookup profile in client namespace
	if clientNs != "" {
		p, err := api.SP().Lister().ServiceProfiles(clientNs).Get(dst)
		if err == nil {
			return p
		}
		if !apierrors.IsNotFound(err) {
			log.Errorf("error getting service profile for %s in %s namespace: %s", dst, clientNs, err)
		}
	}
	// Second, attempt to lookup profile in server namespace
	if svc.Namespace != clientNs {
		p, err := api.SP().Lister().ServiceProfiles(svc.Namespace).Get(dst)
		if err == nil {
			return p
		}
		if !apierrors.IsNotFound(err) {
			log.Errorf("error getting service profile for %s in %s namespace: %s", dst, svc.Namespace, err)
		}
	}
	// Not found; return default.
	log.Debugf("no Service Profile found for '%s' -- using default", dst)
	return &spv1alpha2.ServiceProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name: dst,
		},
		Spec: spv1alpha2.ServiceProfileSpec{
			Routes: []*spv1alpha2.RouteSpec{},
		},
	}
}

func hasOverlap(as, bs []*corev1.Pod) bool {
	for _, a := range as {
		for _, b := range bs {
			if a.Name == b.Name {
				return true
			}
		}
	}
	return false
}

func isPendingOrRunning(pod *corev1.Pod) bool {
	pending := pod.Status.Phase == corev1.PodPending
	running := pod.Status.Phase == corev1.PodRunning
	terminating := pod.DeletionTimestamp != nil
	return (pending || running) && !terminating
}

func isFailed(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodFailed
}
