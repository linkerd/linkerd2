package k8s

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestNewFakeClientSets(t *testing.T) {
	testCases := []struct {
		k8sConfigs []string
		err        error
	}{
		{
			[]string{
				`kind: Secret
apiVersion: v1
metadata:
  name: fake-secret
  namespace: ns
data:
  foo: YmFyCg==`,
			},
			nil,
		},
		{
			[]string{`
apiVersion: linkerd.io/v1alpha1
kind: ServiceProfile
metadata:
  name: foobar.ns.svc.cluster.local
  namespace: linkerd
spec:
  routes:
  - condition:
      pathRegex: "/x/y/z"`,
			},
			nil,
		},
		{
			[]string{""},
			runtime.NewMissingKindErr(""),
		},
	}

	for i, tc := range testCases {
		tc := tc // pin

		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			_, _, err := NewFakeClientSets(tc.k8sConfigs...)
			if !reflect.DeepEqual(err, tc.err) {
				t.Fatalf("Expected error: %s, Got: %s", tc.err, err)
			}
		})
	}
}

func TestNewFakeClientSetsFromManifests(t *testing.T) {
	testCases := []struct {
		manifests []string
		err       error
	}{
		{
			[]string{
				`kind: Secret
apiVersion: v1
metadata:
  name: fake-secret
  namespace: ns
data:
  foo: YmFyCg==`,
			},
			nil,
		},
		{
			[]string{`
apiVersion: linkerd.io/v1alpha1
kind: ServiceProfile
metadata:
  name: foobar.ns.svc.cluster.local
  namespace: linkerd
spec:
  routes:
  - condition:
      pathRegex: "/x/y/z"`,
			},
			nil,
		},
		{
			[]string{`
kind: List
apiVersion: v1
items:
- kind: Secret
  apiVersion: v1
  metadata:
    name: fake-secret
    namespace: ns
  data:
    foo: YmFyCg==
- apiVersion: linkerd.io/v1alpha1
  kind: ServiceProfile
  metadata:
    name: foobar.ns.svc.cluster.local
    namespace: linkerd
  spec:
    routes:
    - condition:
        pathRegex: "/x/y/z"`,
			},
			nil,
		},
		{
			[]string{"---"},
			nil,
		},
	}

	for i, tc := range testCases {
		tc := tc // pin

		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			readers := []io.Reader{}
			for _, m := range tc.manifests {
				readers = append(readers, strings.NewReader(m))
			}

			_, _, err := NewFakeClientSetsFromManifests(readers)
			if !reflect.DeepEqual(err, tc.err) {
				t.Fatalf("Expected error: %s, Got: %s", tc.err, err)
			}
		})
	}
}

func TestToRuntimeObject(t *testing.T) {
	testCases := []struct {
		config string
		err    error
	}{
		{
			`kind: ConfigMap
apiVersion: v1
metadata:
  name: fake-cm
  namespace: ns
data:
  foo: bar`,
			nil,
		},
		{
			`kind: Secret
apiVersion: v1
metadata:
  name: fake-secret
  namespace: ns
data:
  foo: YmFyCg==`,
			nil,
		},
		{
			`
apiVersion: linkerd.io/v1alpha1
kind: ServiceProfile
metadata:
  name: foobar.ns.svc.cluster.local
  namespace: linkerd
spec:
  routes:
  - condition:
      pathRegex: "/x/y/z"`,
			nil,
		},
		{
			"",
			runtime.NewMissingKindErr(""),
		},
		{
			"---",
			runtime.NewMissingKindErr("---"),
		},
		{
			`apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: fakecrd.linkerd.io
spec:
  group: my-group.io
  version: v1alpha1
  scope: Namespaced
  names:
    plural: fakecrds
    singular: fakecrd
    kind: FakeCRD
    shortNames:
    - fc`,
			runtime.NewNotRegisteredGVKErrForTarget(
				"k8s.io/client-go/kubernetes/scheme/register.go:61",
				schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1beta1", Kind: "CustomResourceDefinition"},
				nil,
			),
		},
	}

	for i, tc := range testCases {
		tc := tc // pin

		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			_, err := ToRuntimeObject(tc.config)
			if !reflect.DeepEqual(err, tc.err) {
				t.Fatalf("Expected error: %s, Got: %s", tc.err, err)
			}
		})
	}
}
