package k8s

import (
	"github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/scheme"
	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
func NewFakeMetadataAPI(configs []string, refs []*corev1.ObjectReference) (*MetadataAPI, error) {
	k8sClient, _, _, _, err := k8s.NewFakeClientSets(configs...)
	if err != nil {
		return nil, err
	}

	sch := scheme.Scheme
	metav1.AddMetaToScheme(sch)

	var rmc []runtime.Object
	for _, ref := range refs {
		meta := &metav1.PartialObjectMetadata{
			TypeMeta: metav1.TypeMeta{
				APIVersion: ref.APIVersion,
				Kind:       ref.Kind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ref.Namespace,
				Name:      ref.Name,
			},
		}
		rmc = append(rmc, meta)
	}

	metadataClient := fake.NewSimpleMetadataClient(sch, rmc...)

	return newClusterScopedMetadataAPI(
		k8sClient,
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
