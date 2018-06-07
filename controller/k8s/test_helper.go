package k8s

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

func toRuntimeObject(config string) (runtime.Object, error) {
	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode([]byte(config), nil, nil)
	return obj, err
}

func NewFakeAPI(configs ...string) (*API, error) {
	objs := []runtime.Object{}
	for _, config := range configs {
		obj, err := toRuntimeObject(config)
		if err != nil {
			return nil, err
		}
		objs = append(objs, obj)
	}

	clientSet := fake.NewSimpleClientset(objs...)
	return NewAPI(clientSet), nil
}
