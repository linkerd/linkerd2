package k8s

import (
	"bufio"
	"io"
	"strings"

	spclient "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	spfake "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/fake"
	spscheme "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/scheme"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"
)

// NewFakeClientSets provides a mock Kubernetes ClientSet.
func NewFakeClientSets(configs ...string) (kubernetes.Interface, spclient.Interface, error) {
	objs := []runtime.Object{}
	spObjs := []runtime.Object{}
	for _, config := range configs {
		obj, err := ToRuntimeObject(config)
		if err != nil {
			return nil, nil, err
		}
		if strings.ToLower(obj.GetObjectKind().GroupVersionKind().Kind) == ServiceProfile {
			spObjs = append(spObjs, obj)
		} else {
			objs = append(objs, obj)
		}
	}

	return fake.NewSimpleClientset(objs...), spfake.NewSimpleClientset(spObjs...), nil
}

// NewFakeClientSetsFromManifests reads from a slice of readers, each
// representing a manifest or collection of manifests, and returns a mock
// Kubernetes ClientSet.
func NewFakeClientSetsFromManifests(readers []io.Reader) (kubernetes.Interface, spclient.Interface, error) {
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
				return nil, nil, err
			}

			// check for kind
			var typeMeta metav1.TypeMeta
			if err := yaml.Unmarshal(bytes, &typeMeta); err != nil {
				return nil, nil, err
			}

			switch typeMeta.Kind {
			case "":
				log.Warnf("Kind missing from YAML, skipping")

			case "CustomResourceDefinition":
				// TODO: support CRDs:
				// apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
				// apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
				// apiextensionsfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
				log.Warnf("CRDs not supported, skipping")

			case "List":
				var sourceList corev1.List
				if err := yaml.Unmarshal(bytes, &sourceList); err != nil {
					return nil, nil, err
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
	// TODO: support CRDs:
	// apiextensionsv1beta1.AddToScheme(scheme.Scheme)
	spscheme.AddToScheme(scheme.Scheme)
	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode([]byte(config), nil, nil)
	return obj, err
}
