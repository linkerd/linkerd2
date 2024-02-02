package k8s

import (
	l5dcrdclient "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	"github.com/linkerd/linkerd2/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientsetscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/metadata/fake"
	"k8s.io/client-go/testing"
)

// NewFakeAPI provides a mock Kubernetes API for testing.
func NewFakeAPI(configs ...string) (*API, error) {
	clientSet, _, _, spClientSet, err := k8s.NewFakeClientSets(configs...)
	if err != nil {
		return nil, err
	}

	return NewFakeClusterScopedAPI(clientSet, spClientSet), nil
}

// NewFakeAPI provides a mock Kubernetes API for testing.
func NewFakeAPIWithActions(configs ...string) (*API, func() []testing.Action, error) {
	clientSet, _, _, spClientSet, err := k8s.NewFakeClientSets(configs...)
	if err != nil {
		return nil, nil, err
	}

	return NewFakeClusterScopedAPI(clientSet, spClientSet), clientSet.Actions, nil
}

// NewFakeAPIWithL5dClient provides a mock Kubernetes API for testing like
// NewFakeAPI, but it also returns the mock client for linkerd CRDs
func NewFakeAPIWithL5dClient(configs ...string) (*API, l5dcrdclient.Interface, error) {
	clientSet, _, _, l5dClientSet, err := k8s.NewFakeClientSets(configs...)
	if err != nil {
		return nil, nil, err
	}

	return NewFakeClusterScopedAPI(clientSet, l5dClientSet), l5dClientSet, nil
}

// NewFakeClusterScopedAPI provides a mock Kubernetes API for testing.
func NewFakeClusterScopedAPI(clientSet kubernetes.Interface, l5dClientSet l5dcrdclient.Interface) *API {
	return NewClusterScopedAPI(
		clientSet,
		nil,
		l5dClientSet,
		"fake",
		CJ,
		CM,
		Deploy,
		DS,
		Endpoint,
		Job,
		MWC,
		NS,
		Pod,
		ExtWorkload,
		RC,
		RS,
		SP,
		SS,
		Svc,
		Node,
		ES,
		Srv,
		Secret,
		ExtWorkload,
	)
}

// NewFakeMetadataAPI provides a mock Kubernetes API for testing.
func NewFakeMetadataAPI(configs []string) (*MetadataAPI, error) {
	sch := runtime.NewScheme()
	metav1.AddMetaToScheme(sch)

	var objs []runtime.Object
	for _, config := range configs {
		obj, err := k8s.ToRuntimeObject(config)
		if err != nil {
			return nil, err
		}
		objMeta, err := toPartialObjectMetadata(obj)
		if err != nil {
			return nil, err
		}
		objs = append(objs, objMeta)
	}

	metadataClient := fake.NewSimpleMetadataClient(sch, objs...)

	return newClusterScopedMetadataAPI(
		metadataClient,
		"fake",
		CJ,
		CM,
		Deploy,
		DS,
		Endpoint,
		Job,
		MWC,
		NS,
		Pod,
		RC,
		RS,
		SP,
		SS,
		Svc,
		Node,
		ES,
		Svc,
	)
}

func toPartialObjectMetadata(obj runtime.Object) (*metav1.PartialObjectMetadata, error) {
	u := &unstructured.Unstructured{}
	err := clientsetscheme.Scheme.Convert(obj, u, nil)
	if err != nil {
		return nil, err
	}

	return &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{
			APIVersion: u.GetAPIVersion(),
			Kind:       u.GetKind(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       u.GetNamespace(),
			Name:            u.GetName(),
			Annotations:     u.GetAnnotations(),
			Labels:          u.GetLabels(),
			OwnerReferences: u.GetOwnerReferences(),
		},
	}, nil
}
