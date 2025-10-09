package k8s

import (
	"context"
	"fmt"
	"strings"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/metadata/metadatainformer"
	"k8s.io/client-go/rest"
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
func InitializeMetadataAPI(kubeConfig string, cluster string, resources ...APIResource) (*MetadataAPI, error) {
	config, err := k8s.GetConfig(kubeConfig, "")
	if err != nil {
		return nil, fmt.Errorf("error configuring Kubernetes API client: %w", err)
	}
	return InitializeMetadataAPIForConfig(config, cluster, resources...)
}

func InitializeMetadataAPIForConfig(kubeConfig *rest.Config, cluster string, resources ...APIResource) (*MetadataAPI, error) {
	client, err := metadata.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	api, err := newClusterScopedMetadataAPI(client, cluster, resources...)
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
	cluster string,
	resources ...APIResource,
) (*MetadataAPI, error) {
	sharedInformers := metadatainformer.NewFilteredSharedInformerFactory(
		metadataClient,
		ResyncTime,
		metav1.NamespaceAll,
		nil,
	)

	api := &MetadataAPI{
		client:          metadataClient,
		inf:             make(map[APIResource]informers.GenericInformer),
		syncChecks:      make([]cache.InformerSynced, 0),
		sharedInformers: sharedInformers,
	}

	informerLabels := prometheus.Labels{
		"cluster": cluster,
	}

	for _, resource := range resources {
		if err := api.addInformer(resource, informerLabels); err != nil {
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

// UnregisterGauges unregisters all the prometheus cache gauges associated to this API
func (api *MetadataAPI) UnregisterGauges() {
	api.promGauges.unregister()
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

// GetRootOwnerKindAndName returns the resource's owner's type and metadata, using owner
// references from the Kubernetes API. Parent refs are recursively traversed to find the
// root parent resource, at least as far as the controller has permissions to query.
// This will attempt to lookup resources through the shared informer cache where possible,
// but may fall back to direct Kubernetes API calls where necessary.
func (api *MetadataAPI) GetRootOwnerKindAndName(ctx context.Context, tm *metav1.TypeMeta, om *metav1.ObjectMeta, retry bool) (*metav1.TypeMeta, *metav1.ObjectMeta) {
	ownerRefs := om.OwnerReferences
	if len(ownerRefs) == 0 {
		// resource without a parent
		log.Debugf("Found root owner ref in ns %s: %s/%s: %s", om.Namespace, tm.APIVersion, tm.Kind, om.Name)
		return tm, om
	} else if len(ownerRefs) > 1 {
		log.Debugf("unexpected owner reference count (%d): %+v", len(ownerRefs), ownerRefs)
		return tm, om
	}

	parentRef := ownerRefs[0]
	parentType := metav1.TypeMeta{Kind: parentRef.Kind, APIVersion: parentRef.APIVersion}
	// The set of resources that we look up in the indexer are fairly niche. They all must be able to:
	// - be a parent of another resource, usually a Pod
	// - have a parent resource themselves
	// Currently, this is limited to Jobs (parented by CronJobs) and ReplicaSets (parented by
	// Deployments, StatefulSets, argo Rollouts, etc.). This list may change in the future, but
	// is sufficient for now.
	switch parentRef.Kind {
	case "Job":
		parent, err := api.getByNamespace(Job, om.Namespace, parentRef.Name)
		if err == nil {
			return api.GetRootOwnerKindAndName(ctx, &parentType, &parent.ObjectMeta, retry)
		}
		log.Warnf("failed to retrieve job from indexer %s/%s: %s", om.Namespace, parentRef.Name, err)
		if !retry {
			return &parentType, &metav1.ObjectMeta{Name: parentRef.Name, Namespace: om.Namespace}
		}
	case "ReplicaSet":
		parent, err := api.getByNamespace(RS, om.Namespace, parentRef.Name)
		if err == nil {
			return api.GetRootOwnerKindAndName(ctx, &parentType, &parent.ObjectMeta, retry)
		}
		log.Warnf("failed to retrieve replicaset from indexer %s/%s: %s", om.Namespace, parentRef.Name, err)
		if !retry {
			return &parentType, &metav1.ObjectMeta{Name: parentRef.Name, Namespace: om.Namespace}
		}
	case "":
		log.Warnf("parent ref has no kind: %v", parentRef)
		return tm, om
	}

	resource := schema.FromAPIVersionAndKind(parentRef.APIVersion, parentRef.Kind).
		GroupVersion().
		WithResource(strings.ToLower(parentRef.Kind) + "s")
	parent, err := api.client.Resource(resource).
		Namespace(om.Namespace).
		Get(ctx, parentRef.Name, metav1.GetOptions{})
	if err != nil {
		log.Warnf("failed to retrieve resource from direct API call %s/%s/%s: %s", schema.FromAPIVersionAndKind(parentRef.APIVersion, parentRef.Kind), om.Namespace, parentRef.Name, err)
		return &parentType, &metav1.ObjectMeta{Name: parentRef.Name, Namespace: om.Namespace}
	}
	return api.GetRootOwnerKindAndName(ctx, &parentType, &parent.ObjectMeta, retry)
}

func (api *MetadataAPI) addInformer(res APIResource, informerLabels prometheus.Labels) error {
	gvk, err := res.GVK()
	if err != nil {
		return err
	}
	gvr, _ := meta.UnsafeGuessKindToResource(gvk)
	inf := api.sharedInformers.ForResource(gvr)
	api.syncChecks = append(api.syncChecks, inf.Informer().HasSynced)
	api.promGauges.addInformerSize(strings.ToLower(gvk.Kind), informerLabels, inf.Informer())
	api.inf[res] = inf

	return nil
}
