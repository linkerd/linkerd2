package k8s

import (
	"github.com/linkerd/linkerd2/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clientsetscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/metadata/fake"
)

// NewFakeAPI provides a mock Kubernetes API for testing.
func NewFakeAPI(configs ...string) (*API, error) {
	clientSet, _, _, spClientSet, err := k8s.NewFakeClientSets(configs...)
	if err != nil {
		return nil, err
	}

	return NewClusterScopedAPI(
		clientSet,
		nil,
		spClientSet,
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
		Srv,
	), nil
}

// NewFakeMetadataAPI provides a mock Kubernetes API for testing.
func NewFakeMetadataAPI(configs []string) (*MetadataAPI, error) {
	sch := clientsetscheme.Scheme
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
			OwnerReferences: u.GetOwnerReferences(),
		},
	}, nil
}
