package k8s

import (
	"context"
	"fmt"
	"strings"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/metadata/metadatainformer"
	"k8s.io/client-go/tools/cache"
)

// MetadataAPI provides shared metadata informers for some Kubernetes resources
type MetadataAPI struct {
	promGauges

	client          metadata.Interface
	inf             map[APIResource]informers.GenericInformer
	syncChecks      []cache.InformerSynced
	sharedInformers metadatainformer.SharedInformerFactory
}

// InitializeMetadataAPI returns an instance of MetadataAPI with metadata
// informers for the provided resources
func InitializeMetadataAPI(kubeConfig string, resources ...APIResource) (*MetadataAPI, error) {
	config, err := k8s.GetConfig(kubeConfig, "")
	if err != nil {
		return nil, fmt.Errorf("error configuring Kubernetes API client: %w", err)
	}

	client, err := metadata.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	api, err := newClusterScopedMetadataAPI(client, resources...)
	if err != nil {
		return nil, err
	}

	for _, gauge := range api.gauges {
		if err := prometheus.Register(gauge); err != nil {
			log.Warnf("failed to register Prometheus gauge %s: %s", gauge.Desc().String(), err)
		}
	}
	return api, nil
}

func newClusterScopedMetadataAPI(
	metadataClient metadata.Interface,
	resources ...APIResource,
) (*MetadataAPI, error) {
	sharedInformers := metadatainformer.NewFilteredSharedInformerFactory(
		metadataClient,
		resyncTime,
		metav1.NamespaceAll,
		nil,
	)

	api := &MetadataAPI{
		client:          metadataClient,
		inf:             make(map[APIResource]informers.GenericInformer),
		syncChecks:      make([]cache.InformerSynced, 0),
		sharedInformers: sharedInformers,
	}

	for _, resource := range resources {
		if err := api.addInformer(resource); err != nil {
			return nil, err
		}
	}
	return api, nil
}

// Sync waits for all informers to be synced.
func (api *MetadataAPI) Sync(stopCh <-chan struct{}) {
	api.sharedInformers.Start(stopCh)

	waitForCacheSync(api.syncChecks)
}

func (api *MetadataAPI) getLister(res APIResource) (cache.GenericLister, error) {
	inf, ok := api.inf[res]
	if !ok {
		return nil, fmt.Errorf("metadata informer (%v) not configured", res)
	}

	return inf.Lister(), nil
}

// Get returns the metadata for the supplied object type and name. This uses a
// shared informer and the results may be out of date if the informer is
// lagging behind.
func (api *MetadataAPI) Get(res APIResource, name string) (*metav1.PartialObjectMetadata, error) {
	ls, err := api.getLister(res)
	if err != nil {
		return nil, err
	}

	obj, err := ls.Get(name)
	if err != nil {
		return nil, err
	}

	// ls' concrete type is metadatalister.metadataListerShim, whose
	// Get method always returns *metav1.PartialObjectMetadata
	nsMeta, ok := obj.(*metav1.PartialObjectMetadata)
	if !ok {
		return nil, fmt.Errorf("couldn't convert obj %v to PartialObjectMetadata", obj)
	}

	return nsMeta, nil
}

func (api *MetadataAPI) getByNamespace(res APIResource, ns, name string) (*metav1.PartialObjectMetadata, error) {
	ls, err := api.getLister(res)
	if err != nil {
		return nil, err
	}

	obj, err := ls.ByNamespace(ns).Get(name)
	if err != nil {
		return nil, err
	}

	nsMeta, ok := obj.(*metav1.PartialObjectMetadata)
	if !ok {
		return nil, fmt.Errorf("couldn't convert obj %v to PartialObjectMetadata", obj)
	}

	return nsMeta, nil
}

// GetByNamespaceFiltered returns a list of Kubernetes object references, given
// a type, namespace, name and label selector. This uses a shared informer and
// the results may be out of date if the informer is lagging behind.
func (api *MetadataAPI) GetByNamespaceFiltered(
	restype APIResource,
	ns string,
	name string,
	labelSelector labels.Selector,
) ([]*metav1.PartialObjectMetadata, error) {
	ls, err := api.getLister(restype)
	if err != nil {
		return nil, err
	}

	var objs []runtime.Object
	if ns == "" {
		objs, err = ls.List(labelSelector)
	} else if name == "" {
		objs, err = ls.ByNamespace(ns).List(labelSelector)
	} else {
		var obj runtime.Object
		obj, err = ls.ByNamespace(ns).Get(name)
		objs = []runtime.Object{obj}
	}

	if err != nil {
		return nil, err
	}

	objMetas := []*metav1.PartialObjectMetadata{}
	for _, obj := range objs {
		// ls' concrete type is metadatalister.metadataListerShim, which
		// guarantees this cast won't fail
		objMeta, ok := obj.(*metav1.PartialObjectMetadata)
		if !ok {
			return nil, fmt.Errorf("couldn't convert obj %v to PartialObjectMetadata", obj)
		}
		gvk, err := restype.GVK()
		if err != nil {
			return nil, err
		}

		// objMeta's TypeMeta fields aren't getting populated, so we do it
		// manually here
		objMeta.SetGroupVersionKind(gvk)
		objMetas = append(objMetas, objMeta)
	}

	return objMetas, nil
}

// GetOwnerKindAndName returns the pod owner's kind and name, using owner
// references from the Kubernetes API. The kind is represented as the
// Kubernetes singular resource type (e.g. deployment, daemonset, job, etc.).
// If retry is true, when the shared informer cache doesn't return anything we
// try again with a direct Kubernetes API call.
func (api *MetadataAPI) GetOwnerKindAndName(ctx context.Context, pod *corev1.Pod, retry bool) (string, string, error) {
	ownerRefs := pod.GetOwnerReferences()
	if len(ownerRefs) == 0 {
		// pod without a parent
		return "pod", pod.Name, nil
	} else if len(ownerRefs) > 1 {
		log.Debugf("unexpected owner reference count (%d): %+v", len(ownerRefs), ownerRefs)
		return "pod", pod.Name, nil
	}

	parent := ownerRefs[0]
	var parentObj metav1.Object
	var err error
	switch parent.Kind {
	case "Job":
		parentObj, err = api.getByNamespace(Job, pod.Namespace, parent.Name)
		if err != nil {
			log.Warnf("failed to retrieve job from indexer %s/%s: %s", pod.Namespace, parent.Name, err)
			if retry {
				parentObj, err = api.client.
					Resource(batchv1.SchemeGroupVersion.WithResource("jobs")).
					Namespace(pod.Namespace).
					Get(ctx, parent.Name, metav1.GetOptions{})
				if err != nil {
					log.Warnf("failed to retrieve job from direct API call %s/%s: %s", pod.Namespace, parent.Name, err)
				}
			}
		}
	case "ReplicaSet":
		var rsObj *metav1.PartialObjectMetadata
		rsObj, err = api.getByNamespace(RS, pod.Namespace, parent.Name)
		if err != nil {
			log.Warnf("failed to retrieve replicaset from indexer %s/%s: %s", pod.Namespace, parent.Name, err)
			if retry {
				rsObj, err = api.client.
					Resource(appsv1.SchemeGroupVersion.WithResource("replicasets")).
					Namespace(pod.Namespace).
					Get(ctx, parent.Name, metav1.GetOptions{})
				if err != nil {
					log.Warnf("failed to retrieve replicaset from direct API call %s/%s: %s", pod.Namespace, parent.Name, err)
				}
			}
		}

		if rsObj == nil || !isValidRSParent(rsObj) {
			return strings.ToLower(parent.Kind), parent.Name, nil
		}
		parentObj = rsObj
	default:
		return strings.ToLower(parent.Kind), parent.Name, nil
	}

	if err == nil && len(parentObj.GetOwnerReferences()) == 1 {
		grandParent := parentObj.GetOwnerReferences()[0]
		return strings.ToLower(grandParent.Kind), grandParent.Name, nil
	}
	return strings.ToLower(parent.Kind), parent.Name, nil
}

func (api *MetadataAPI) addInformer(res APIResource) error {
	gvk, err := res.GVK()
	if err != nil {
		return err
	}
	gvr, _ := meta.UnsafeGuessKindToResource(gvk)
	inf := api.sharedInformers.ForResource(gvr)
	api.syncChecks = append(api.syncChecks, inf.Informer().HasSynced)
	api.promGauges.addInformerSize(strings.ToLower(gvk.Kind), inf.Informer())
	api.inf[res] = inf

	return nil
}
