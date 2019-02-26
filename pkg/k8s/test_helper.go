package k8s

import (
	"strings"

	spclient "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	spfake "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/fake"
	spscheme "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/scheme"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

// NewFakeClientSets provides a mock Kubernetes ClientSet for testing.
func NewFakeClientSets(configs ...string) (kubernetes.Interface, spclient.Interface) {
	objs := []runtime.Object{}
	spObjs := []runtime.Object{}
	for _, config := range configs {
		obj, err := ToRuntimeObject(config)
		if err != nil {
			return nil, nil
		}
		if strings.ToLower(obj.GetObjectKind().GroupVersionKind().Kind) == ServiceProfile {
			spObjs = append(spObjs, obj)
		} else {
			objs = append(objs, obj)
		}
	}

	return fake.NewSimpleClientset(objs...), spfake.NewSimpleClientset(spObjs...)
}

// ToRuntimeObject deserializes Kubernetes YAML into a Runtime Object
func ToRuntimeObject(config string) (runtime.Object, error) {
	spscheme.AddToScheme(scheme.Scheme)
	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode([]byte(config), nil, nil)
	return obj, err
}
