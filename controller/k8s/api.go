package k8s

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	spv1alpha2 "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	l5dcrdclient "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	l5dcrdinformer "github.com/linkerd/linkerd2/controller/gen/client/informers/externalversions"
	srvinformers "github.com/linkerd/linkerd2/controller/gen/client/informers/externalversions/server/v1beta1"
	spinformers "github.com/linkerd/linkerd2/controller/gen/client/informers/externalversions/serviceprofile/v1alpha2"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/client_golang/prometheus"
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
	arinformers "k8s.io/client-go/informers/admissionregistration/v1"
	appv1informers "k8s.io/client-go/informers/apps/v1"
	batchv1informers "k8s.io/client-go/informers/batch/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	discoveryinformers "k8s.io/client-go/informers/discovery/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// API provides shared informers for all Kubernetes objects
type API struct {
	promGauges

	Client        kubernetes.Interface
	DynamicClient dynamic.Interface

	cj       batchv1informers.CronJobInformer
	cm       coreinformers.ConfigMapInformer
	deploy   appv1informers.DeploymentInformer
	ds       appv1informers.DaemonSetInformer
	endpoint coreinformers.EndpointsInformer
	es       discoveryinformers.EndpointSliceInformer
	job      batchv1informers.JobInformer
	mwc      arinformers.MutatingWebhookConfigurationInformer
	ns       coreinformers.NamespaceInformer
	pod      coreinformers.PodInformer
	rc       coreinformers.ReplicationControllerInformer
	rs       appv1informers.ReplicaSetInformer
	sp       spinformers.ServiceProfileInformer
	ss       appv1informers.StatefulSetInformer
	svc      coreinformers.ServiceInformer
	node     coreinformers.NodeInformer
	secret   coreinformers.SecretInformer
	srv      srvinformers.ServerInformer

	syncChecks            []cache.InformerSynced
	sharedInformers       informers.SharedInformerFactory
	l5dCrdSharedInformers l5dcrdinformer.SharedInformerFactory
}

// InitializeAPI creates Kubernetes clients and returns an initialized API wrapper.
func InitializeAPI(ctx context.Context, kubeConfig string, ensureClusterWideAccess bool, resources ...APIResource) (*API, error) {
	config, err := k8s.GetConfig(kubeConfig, "")
	if err != nil {
		return nil, fmt.Errorf("error configuring Kubernetes API client: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	k8sClient, err := k8s.NewAPIForConfig(config, "", []string{}, 0)
	if err != nil {
		return nil, err
	}

	return initAPI(ctx, k8sClient, dynamicClient, config, ensureClusterWideAccess, resources...)
}

// InitializeAPIForConfig creates Kubernetes clients and returns an initialized API wrapper.
func InitializeAPIForConfig(ctx context.Context, kubeConfig *rest.Config, ensureClusterWideAccess bool, resources ...APIResource) (*API, error) {
	k8sClient, err := k8s.NewAPIForConfig(kubeConfig, "", []string{}, 0)
	if err != nil {
		return nil, err
	}

	return initAPI(ctx, k8sClient, nil, kubeConfig, ensureClusterWideAccess, resources...)
}

func initAPI(ctx context.Context, k8sClient *k8s.KubernetesAPI, dynamicClient dynamic.Interface, kubeConfig *rest.Config, ensureClusterWideAccess bool, resources ...APIResource) (*API, error) {
	// check for cluster-wide access
	var err error

	if ensureClusterWideAccess {
		err := k8s.ClusterAccess(ctx, k8sClient)
		if err != nil {
			return nil, err
		}
	}

	// check for need and access to Linkerd CRD clients
	var l5dCrdClient *l5dcrdclient.Clientset
	for _, res := range resources {
		switch {
		case res == SP:
			err := k8s.ServiceProfilesAccess(ctx, k8sClient)
			if err != nil {
				return nil, err
			}
		case res == Srv:
			err := k8s.ServersAccess(ctx, k8sClient)
			if err != nil {
				return nil, err
			}
		default:
			continue
		}
		l5dCrdClient, err = NewL5DCRDClient(kubeConfig)
		if err != nil {
			return nil, err
		}
		break
	}

	api := NewClusterScopedAPI(k8sClient, dynamicClient, l5dCrdClient, resources...)
	for _, gauge := range api.gauges {
		if err := prometheus.Register(gauge); err != nil {
			log.Warnf("failed to register Prometheus gauge %s: %s", gauge.Desc().String(), err)
		}
	}
	return api, nil
}

// NewClusterScopedAPI takes a Kubernetes client and returns an initialized cluster-wide API.
func NewClusterScopedAPI(
	k8sClient kubernetes.Interface,
	dynamicClient dynamic.Interface,
	l5dCrdClient l5dcrdclient.Interface,
	resources ...APIResource,
) *API {
	sharedInformers := informers.NewSharedInformerFactory(k8sClient, resyncTime)
	return newAPI(k8sClient, dynamicClient, l5dCrdClient, sharedInformers, resources...)
}

// NewNamespacedAPI takes a Kubernetes client and returns an initialized API scoped to namespace.
func NewNamespacedAPI(
	k8sClient kubernetes.Interface,
	dynamicClient dynamic.Interface,
	l5dCrdClient l5dcrdclient.Interface,
	namespace string,
	resources ...APIResource,
) *API {
	sharedInformers := informers.NewSharedInformerFactoryWithOptions(k8sClient, resyncTime, informers.WithNamespace(namespace))
	return newAPI(k8sClient, dynamicClient, l5dCrdClient, sharedInformers, resources...)
}

// newAPI takes a Kubernetes client and returns an initialized API.
func newAPI(
	k8sClient kubernetes.Interface,
	dynamicClient dynamic.Interface,
	l5dCrdClient l5dcrdclient.Interface,
	sharedInformers informers.SharedInformerFactory,
	resources ...APIResource,
) *API {
	var l5dCrdSharedInformers l5dcrdinformer.SharedInformerFactory
	if l5dCrdClient != nil {
		l5dCrdSharedInformers = l5dcrdinformer.NewSharedInformerFactory(l5dCrdClient, resyncTime)
	}

	api := &API{
		Client:                k8sClient,
		DynamicClient:         dynamicClient,
		syncChecks:            make([]cache.InformerSynced, 0),
		sharedInformers:       sharedInformers,
		l5dCrdSharedInformers: l5dCrdSharedInformers,
	}

	for _, resource := range resources {
		switch resource {
		case CJ:
			api.cj = sharedInformers.Batch().V1().CronJobs()
			api.syncChecks = append(api.syncChecks, api.cj.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.CronJob, api.cj.Informer())
		case CM:
			api.cm = sharedInformers.Core().V1().ConfigMaps()
			api.syncChecks = append(api.syncChecks, api.cm.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.ConfigMap, api.cm.Informer())
		case Deploy:
			api.deploy = sharedInformers.Apps().V1().Deployments()
			api.syncChecks = append(api.syncChecks, api.deploy.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.Deployment, api.deploy.Informer())
		case DS:
			api.ds = sharedInformers.Apps().V1().DaemonSets()
			api.syncChecks = append(api.syncChecks, api.ds.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.DaemonSet, api.ds.Informer())
		case Endpoint:
			api.endpoint = sharedInformers.Core().V1().Endpoints()
			api.syncChecks = append(api.syncChecks, api.endpoint.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.Endpoints, api.endpoint.Informer())
		case ES:
			api.es = sharedInformers.Discovery().V1().EndpointSlices()
			api.syncChecks = append(api.syncChecks, api.es.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.EndpointSlices, api.es.Informer())
		case Job:
			api.job = sharedInformers.Batch().V1().Jobs()
			api.syncChecks = append(api.syncChecks, api.job.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.Job, api.job.Informer())
		case MWC:
			api.mwc = sharedInformers.Admissionregistration().V1().MutatingWebhookConfigurations()
			api.syncChecks = append(api.syncChecks, api.mwc.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.MutatingWebhookConfig, api.mwc.Informer())
		case NS:
			api.ns = sharedInformers.Core().V1().Namespaces()
			api.syncChecks = append(api.syncChecks, api.ns.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.Namespace, api.ns.Informer())
		case Pod:
			api.pod = sharedInformers.Core().V1().Pods()
			api.syncChecks = append(api.syncChecks, api.pod.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.Pod, api.pod.Informer())
		case RC:
			api.rc = sharedInformers.Core().V1().ReplicationControllers()
			api.syncChecks = append(api.syncChecks, api.rc.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.ReplicationController, api.rc.Informer())
		case RS:
			api.rs = sharedInformers.Apps().V1().ReplicaSets()
			api.syncChecks = append(api.syncChecks, api.rs.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.ReplicaSet, api.rs.Informer())
		case SP:
			if l5dCrdSharedInformers == nil {
				panic("Linkerd CRD shared informer not configured")
			}
			api.sp = l5dCrdSharedInformers.Linkerd().V1alpha2().ServiceProfiles()
			api.syncChecks = append(api.syncChecks, api.sp.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.ServiceProfile, api.sp.Informer())
		case Srv:
			if l5dCrdSharedInformers == nil {
				panic("Linkerd CRD shared informer not configured")
			}
			api.srv = l5dCrdSharedInformers.Server().V1beta1().Servers()
			api.syncChecks = append(api.syncChecks, api.srv.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.Server, api.srv.Informer())
		case SS:
			api.ss = sharedInformers.Apps().V1().StatefulSets()
			api.syncChecks = append(api.syncChecks, api.ss.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.StatefulSet, api.ss.Informer())
		case Svc:
			api.svc = sharedInformers.Core().V1().Services()
			api.syncChecks = append(api.syncChecks, api.svc.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.Service, api.svc.Informer())
		case Node:
			api.node = sharedInformers.Core().V1().Nodes()
			api.syncChecks = append(api.syncChecks, api.node.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.Node, api.node.Informer())
		case Secret:
			api.secret = sharedInformers.Core().V1().Secrets()
			api.syncChecks = append(api.syncChecks, api.secret.Informer().HasSynced)
			api.promGauges.addInformerSize(k8s.Secret, api.secret.Informer())
		}
	}
	return api
}

// Sync waits for all informers to be synced.
func (api *API) Sync(stopCh <-chan struct{}) {
	api.sharedInformers.Start(stopCh)

	if api.l5dCrdSharedInformers != nil {
		api.l5dCrdSharedInformers.Start(stopCh)
	}

	waitForCacheSync(api.syncChecks)
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

// ES provides access to a shared informer and lister for EndpointSlices
func (api *API) ES() discoveryinformers.EndpointSliceInformer {
	if api.es == nil {
		panic("EndpointSlices informer not configured")
	}
	return api.es
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

// Srv provides access to a shared informer and lister for Servers.
func (api *API) Srv() srvinformers.ServerInformer {
	if api.srv == nil {
		panic("Srv informer not configured")
	}
	return api.srv
}

// MWC provides access to a shared informer and lister for MutatingWebhookConfigurations.
func (api *API) MWC() arinformers.MutatingWebhookConfigurationInformer {
	if api.mwc == nil {
		panic("MWC informer not configured")
	}
	return api.mwc
}

// Job provides access to a shared informer and lister for Jobs.
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

// Node provides access to a shared informer and lister for Nodes.
func (api *API) Node() coreinformers.NodeInformer {
	if api.node == nil {
		panic("Node informer not configured")
	}
	return api.node
}

// Secret provides access to a shared informer and lister for Secrets.
func (api *API) Secret() coreinformers.SecretInformer {
	if api.secret == nil {
		panic("Secret informer not configured")
	}
	return api.secret
}

// CJ provides access to a shared informer and lister for CronJobs.
func (api *API) CJ() batchv1informers.CronJobInformer {
	if api.cj == nil {
		panic("CJ informer not configured")
	}
	return api.cj
}

// GetObjects returns a list of Kubernetes objects, given a namespace, type, name and label selector.
// If namespace is an empty string, match objects in all namespaces.
// If name is an empty string, match all objects of the given type.
// If label selector is an empty string, match all labels.
func (api *API) GetObjects(namespace, restype, name string, label labels.Selector) ([]runtime.Object, error) {
	switch restype {
	case k8s.Namespace:
		return api.getNamespaces(name, label)
	case k8s.CronJob:
		return api.getCronjobs(namespace, name, label)
	case k8s.DaemonSet:
		return api.getDaemonsets(namespace, name, label)
	case k8s.Deployment:
		return api.getDeployments(namespace, name, label)
	case k8s.Job:
		return api.getJobs(namespace, name, label)
	case k8s.Pod:
		return api.getPods(namespace, name, label)
	case k8s.ReplicationController:
		return api.getRCs(namespace, name, label)
	case k8s.ReplicaSet:
		return api.getReplicasets(namespace, name, label)
	case k8s.Service:
		return api.getServices(namespace, name)
	case k8s.StatefulSet:
		return api.getStatefulsets(namespace, name, label)
	default:
		return nil, status.Errorf(codes.Unimplemented, "unimplemented resource type: %s", restype)
	}
}

// KindSupported returns true if there is an informer configured for the
// specified resource type.
func (api *API) KindSupported(restype string) bool {
	switch restype {
	case k8s.Namespace:
		return api.ns != nil
	case k8s.CronJob:
		return api.cj != nil
	case k8s.DaemonSet:
		return api.ds != nil
	case k8s.Deployment:
		return api.deploy != nil
	case k8s.Job:
		return api.job != nil
	case k8s.Pod:
		return api.pod != nil
	case k8s.ReplicationController:
		return api.rc != nil
	case k8s.ReplicaSet:
		return api.rs != nil
	case k8s.Service:
		return api.svc != nil
	case k8s.StatefulSet:
		return api.ss != nil
	default:
		return false
	}
}

// GetOwnerKindAndName returns the pod owner's kind and name, using owner
// references from the Kubernetes API. The kind is represented as the Kubernetes
// singular resource type (e.g. deployment, daemonset, job, etc.).
// If retry is true, when the shared informer cache doesn't return anything
// we try again with a direct Kubernetes API call.
func (api *API) GetOwnerKindAndName(ctx context.Context, pod *corev1.Pod, retry bool) (string, string) {
	ownerRefs := pod.GetOwnerReferences()
	if len(ownerRefs) == 0 {
		// pod without a parent
		return k8s.Pod, pod.Name
	} else if len(ownerRefs) > 1 {
		log.Debugf("unexpected owner reference count (%d): %+v", len(ownerRefs), ownerRefs)
		return k8s.Pod, pod.Name
	}

	parent := ownerRefs[0]
	var parentObj metav1.Object
	var err error
	switch parent.Kind {
	case "Job":
		parentObj, err = api.Job().Lister().Jobs(pod.Namespace).Get(parent.Name)
		if err != nil {
			log.Warnf("failed to retrieve job from indexer %s/%s: %s", pod.Namespace, parent.Name, err)
			if retry {
				parentObj, err = api.Client.BatchV1().Jobs(pod.Namespace).Get(ctx, parent.Name, metav1.GetOptions{})
				if err != nil {
					log.Warnf("failed to retrieve job from direct API call %s/%s: %s", pod.Namespace, parent.Name, err)
				}
			}
		}
	case "ReplicaSet":
		rsObj, err := api.RS().Lister().ReplicaSets(pod.Namespace).Get(parent.Name)
		if err != nil {
			log.Warnf("failed to retrieve replicaset from indexer %s/%s: %s", pod.Namespace, parent.Name, err)
			if retry {
				rsObj, err = api.Client.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, parent.Name, metav1.GetOptions{})
				if err != nil {
					log.Warnf("failed to retrieve replicaset from direct API call %s/%s: %s", pod.Namespace, parent.Name, err)
				}
			}
		}

		if rsObj == nil || !isValidRSParent(rsObj.GetObjectMeta()) {
			return strings.ToLower(parent.Kind), parent.Name
		}
		parentObj = rsObj

	default:
		return strings.ToLower(parent.Kind), parent.Name
	}

	if err == nil && len(parentObj.GetOwnerReferences()) == 1 {
		grandParent := parentObj.GetOwnerReferences()[0]
		return strings.ToLower(grandParent.Kind), grandParent.Name
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

	case *batchv1.CronJob:
		namespace = typed.Namespace
		selector = labels.Everything()
		jobs, err := api.Job().Lister().Jobs(namespace).List(selector)
		if err != nil {
			return nil, err
		}
		for _, job := range jobs {
			if isOwner(typed.UID, job.GetOwnerReferences()) {
				jobPods, err := api.GetPodsFor(job, includeFailed)
				if err != nil {
					return nil, err
				}
				pods = append(pods, jobPods...)
			}
		}
		return pods, nil

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
		if typed.Spec.Selector == nil {
			selector = labels.Nothing()
		} else {
			selector = labels.Set(typed.Spec.Selector).AsSelector()
		}

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
			if ownerUID == "" || isOwner(ownerUID, pod.GetOwnerReferences()) {
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

	case *batchv1.CronJob:
		return typed.Name, typed.Namespace, nil

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
func (api *API) getNamespaces(name string, labelSelector labels.Selector) ([]runtime.Object, error) {
	var namespaces []*corev1.Namespace

	if name == "" {
		var err error
		namespaces, err = api.NS().Lister().List(labelSelector)
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

func (api *API) getDeployments(namespace, name string, labelSelector labels.Selector) ([]runtime.Object, error) {
	var err error
	var deploys []*appsv1.Deployment

	if namespace == "" {
		deploys, err = api.Deploy().Lister().List(labelSelector)
	} else if name == "" {
		deploys, err = api.Deploy().Lister().Deployments(namespace).List(labelSelector)
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

func (api *API) getPods(namespace, name string, labelSelector labels.Selector) ([]runtime.Object, error) {
	var err error
	var pods []*corev1.Pod

	if namespace == "" {
		pods, err = api.Pod().Lister().List(labelSelector)
	} else if name == "" {
		pods, err = api.Pod().Lister().Pods(namespace).List(labelSelector)
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

func (api *API) getRCs(namespace, name string, labelSelector labels.Selector) ([]runtime.Object, error) {
	var err error
	var rcs []*corev1.ReplicationController

	if namespace == "" {
		rcs, err = api.RC().Lister().List(labelSelector)
	} else if name == "" {
		rcs, err = api.RC().Lister().ReplicationControllers(namespace).List(labelSelector)
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

func (api *API) getDaemonsets(namespace, name string, labelSelector labels.Selector) ([]runtime.Object, error) {
	var err error
	var daemonsets []*appsv1.DaemonSet

	if namespace == "" {
		daemonsets, err = api.DS().Lister().List(labelSelector)
	} else if name == "" {
		daemonsets, err = api.DS().Lister().DaemonSets(namespace).List(labelSelector)
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

func (api *API) getStatefulsets(namespace, name string, labelSelector labels.Selector) ([]runtime.Object, error) {
	var err error
	var statefulsets []*appsv1.StatefulSet

	if namespace == "" {
		statefulsets, err = api.SS().Lister().List(labelSelector)
	} else if name == "" {
		statefulsets, err = api.SS().Lister().StatefulSets(namespace).List(labelSelector)
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

func (api *API) getJobs(namespace, name string, labelSelector labels.Selector) ([]runtime.Object, error) {
	var err error
	var jobs []*batchv1.Job

	if namespace == "" {
		jobs, err = api.Job().Lister().List(labelSelector)
	} else if name == "" {
		jobs, err = api.Job().Lister().Jobs(namespace).List(labelSelector)
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

func (api *API) getCronjobs(namespace, name string, labelSelector labels.Selector) ([]runtime.Object, error) {
	var err error
	var cronjobs []*batchv1.CronJob

	if namespace == "" {
		cronjobs, err = api.CJ().Lister().List(labelSelector)
	} else if name == "" {
		cronjobs, err = api.CJ().Lister().CronJobs(namespace).List(labelSelector)
	} else {
		var cronjob *batchv1.CronJob
		cronjob, err = api.CJ().Lister().CronJobs(namespace).Get(name)
		cronjobs = []*batchv1.CronJob{cronjob}
	}
	if err != nil {
		return nil, err
	}

	objects := []runtime.Object{}
	for _, cronjob := range cronjobs {
		objects = append(objects, cronjob)
	}

	return objects, nil
}

func (api *API) getReplicasets(namespace, name string, labelSelector labels.Selector) ([]runtime.Object, error) {
	var err error
	var replicasets []*appsv1.ReplicaSet

	if namespace == "" {
		replicasets, err = api.RS().Lister().List(labelSelector)
	} else if name == "" {
		replicasets, err = api.RS().Lister().ReplicaSets(namespace).List(labelSelector)
	} else {
		var replicaset *appsv1.ReplicaSet
		replicaset, err = api.RS().Lister().ReplicaSets(namespace).Get(name)
		replicasets = []*appsv1.ReplicaSet{replicaset}
	}
	if err != nil {
		return nil, err
	}

	objects := []runtime.Object{}
	for _, replicaset := range replicasets {
		objects = append(objects, replicaset)
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
		svcPods, err := api.GetPodsFor(svc, includeFailed)
		if err != nil {
			return nil, err
		}

		if hasOverlap(pods, svcPods) {
			services = append(services, svc)
		}
	}
	return services, nil
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
