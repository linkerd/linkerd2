package k8s

import (
	"bufio"
	"io"
	"strings"

	spclient "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	spfake "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/fake"
	tsclient "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	tsfake "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned/fake"

	spscheme "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/scheme"
	tsscheme "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned/scheme"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextensionsfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"
	discoveryfake "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	apiregistrationclient "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"
	apiregistrationfake "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/fake"
	"sigs.k8s.io/yaml"
)

// NewFakeAPI provides a mock KubernetesAPI backed by hard-coded resources
func NewFakeAPI(configs ...string) (*KubernetesAPI, error) {
	client, apiextClient, apiregClient, _, _, err := NewFakeClientSets(configs...)
	if err != nil {
		return nil, err
	}

	return &KubernetesAPI{
		Config:          &rest.Config{},
		Interface:       client,
		Apiextensions:   apiextClient,
		Apiregistration: apiregClient,
	}, nil
}

// NewFakeAPIFromManifests reads from a slice of readers, each representing a
// manifest or collection of manifests, and returns a mock KubernetesAPI.
func NewFakeAPIFromManifests(readers []io.Reader) (*KubernetesAPI, error) {
	client, apiextClient, apiregClient, _, _, err := newFakeClientSetsFromManifests(readers)
	if err != nil {
		return nil, err
	}

	return &KubernetesAPI{
		Interface:       client,
		Apiextensions:   apiextClient,
		Apiregistration: apiregClient,
	}, nil
}

// NewFakeClientSets provides mock Kubernetes ClientSets.
// TODO: make this private once KubernetesAPI (and NewFakeAPI) supports spClient
func NewFakeClientSets(configs ...string) (
	kubernetes.Interface,
	apiextensionsclient.Interface,
	apiregistrationclient.Interface,
	spclient.Interface,
	tsclient.Interface,
	error,
) {
	objs := []runtime.Object{}
	apiextObjs := []runtime.Object{}
	apiRegObjs := []runtime.Object{}
	discoveryObjs := []runtime.Object{}
	spObjs := []runtime.Object{}
	tsObjs := []runtime.Object{}
	for _, config := range configs {
		obj, err := ToRuntimeObject(config)
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}
		switch strings.ToLower(obj.GetObjectKind().GroupVersionKind().Kind) {
		case "customresourcedefinition":
			apiextObjs = append(apiextObjs, obj)
		case "apiservice":
			apiRegObjs = append(apiRegObjs, obj)
		case "apiresourcelist":
			discoveryObjs = append(discoveryObjs, obj)
		case ServiceProfile:
			spObjs = append(spObjs, obj)
		case TrafficSplit:
			tsObjs = append(tsObjs, obj)
		default:
			objs = append(objs, obj)
		}
	}

	cs := fake.NewSimpleClientset(objs...)
	fakeDiscoveryClient := cs.Discovery().(*discoveryfake.FakeDiscovery)
	for _, obj := range discoveryObjs {
		apiResList := obj.(*metav1.APIResourceList)
		fakeDiscoveryClient.Resources = append(fakeDiscoveryClient.Resources, apiResList)
	}

	return cs,
		apiextensionsfake.NewSimpleClientset(apiextObjs...),
		apiregistrationfake.NewSimpleClientset(apiRegObjs...),
		spfake.NewSimpleClientset(spObjs...),
		tsfake.NewSimpleClientset(tsObjs...),
		nil
}

// newFakeClientSetsFromManifests reads from a slice of readers, each
// representing a manifest or collection of manifests, and returns a mock
// Kubernetes ClientSet.
//nolint:unparam
func newFakeClientSetsFromManifests(readers []io.Reader) (
	kubernetes.Interface,
	apiextensionsclient.Interface,
	apiregistrationclient.Interface,
	spclient.Interface,
	tsclient.Interface,
	error,
) {
	configs := []string{}

	for _, reader := range readers {
		r := yamlDecoder.NewYAMLReader(bufio.NewReaderSize(reader, 4096))

		// Iterate over all YAML objects in the input
		for {
			// Read a single YAML object
			bytes, err := r.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, nil, nil, nil, nil, err
			}

			// check for kind
			var typeMeta metav1.TypeMeta
			if err := yaml.Unmarshal(bytes, &typeMeta); err != nil {
				return nil, nil, nil, nil, nil, err
			}

			switch typeMeta.Kind {
			case "":
				// Kind missing from YAML, skipping

			case "List":
				var sourceList corev1.List
				if err := yaml.Unmarshal(bytes, &sourceList); err != nil {
					return nil, nil, nil, nil, nil, err
				}
				for _, item := range sourceList.Items {
					configs = append(configs, string(item.Raw))
				}

			default:
				configs = append(configs, string(bytes))
			}
		}
	}

	return NewFakeClientSets(configs...)
}

// ToRuntimeObject deserializes Kubernetes YAML into a Runtime Object
func ToRuntimeObject(config string) (runtime.Object, error) {
	apiextensionsv1beta1.AddToScheme(scheme.Scheme)
	apiregistrationv1.AddToScheme(scheme.Scheme)
	spscheme.AddToScheme(scheme.Scheme)
	tsscheme.AddToScheme(scheme.Scheme)
	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode([]byte(config), nil, nil)
	return obj, err
}

// ObjectKinds wraps client-go's scheme.Scheme.ObjectKinds()
// It returns all possible group,version,kind of the go object, true if the
// object is considered unversioned, or an error if it's not a pointer or is
// unregistered.
func ObjectKinds(obj runtime.Object) ([]schema.GroupVersionKind, bool, error) {
	apiextensionsv1beta1.AddToScheme(scheme.Scheme)
	apiregistrationv1.AddToScheme(scheme.Scheme)
	spscheme.AddToScheme(scheme.Scheme)
	tsscheme.AddToScheme(scheme.Scheme)
	return scheme.Scheme.ObjectKinds(obj)
}
