package k8s

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestNewFakeAPI(t *testing.T) {

	k8sConfigs := []string{
		`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dep-name
  namespace: dep-ns
`, `
apiVersion: apiextensions.k8s.io/v1beta1
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
    - fc
`, `
apiVersion: apiregistration.k8s.io/v1
kind: APIService
metadata:
  name: v1alpha1.tap.linkerd.io
  labels:
    linkerd.io/control-plane-component: tap
    linkerd.io/control-plane-ns: linkerd
spec:
  group: tap.linkerd.io
  version: v1alpha1
  groupPriorityMinimum: 1000
  versionPriority: 100
  service:
    name: linkerd-tap
    namespace: linkerd
  caBundle: dGFwIGNydA==`,
	}

	api, err := NewFakeAPI(k8sConfigs...)
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	ctx := context.Background()
	deploy, err := api.AppsV1().Deployments("dep-ns").Get(ctx, "dep-name", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	gvk := schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}
	if !reflect.DeepEqual(deploy.GroupVersionKind(), gvk) {
		t.Fatalf("Expected: %s Got: %s", gvk, deploy.GroupVersionKind())
	}

	crd, err := api.Apiextensions.ApiextensionsV1beta1().CustomResourceDefinitions().Get(ctx, "fakecrd.linkerd.io", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	gvk = schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1beta1",
		Kind:    "CustomResourceDefinition",
	}
	if !reflect.DeepEqual(crd.GroupVersionKind(), gvk) {
		t.Fatalf("Expected: %s Got: %s", gvk, crd.GroupVersionKind())
	}
}

func TestNewFakeAPIFromManifests(t *testing.T) {
	k8sConfigs := []string{
		`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dep-name
  namespace: dep-ns
`, `
apiVersion: apiextensions.k8s.io/v1beta1
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
    - fc
`, `
apiVersion: apiregistration.k8s.io/v1
kind: APIService
metadata:
  name: v1alpha1.tap.linkerd.io
  labels:
    linkerd.io/control-plane-component: tap
    linkerd.io/control-plane-ns: linkerd
spec:
  group: tap.linkerd.io
  version: v1alpha1
  groupPriorityMinimum: 1000
  versionPriority: 100
  service:
    name: linkerd-tap
    namespace: linkerd
  caBundle: dGFwIGNydA==`,
	}

	readers := []io.Reader{}
	for _, m := range k8sConfigs {
		readers = append(readers, strings.NewReader(m))
	}

	api, err := NewFakeAPIFromManifests(readers)
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	ctx := context.Background()
	deploy, err := api.AppsV1().Deployments("dep-ns").Get(ctx, "dep-name", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	gvk := schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}
	if !reflect.DeepEqual(deploy.GroupVersionKind(), gvk) {
		t.Fatalf("Expected: %s Got: %s", gvk, deploy.GroupVersionKind())
	}

	crd, err := api.Apiextensions.ApiextensionsV1beta1().CustomResourceDefinitions().Get(ctx, "fakecrd.linkerd.io", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	gvk = schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1beta1",
		Kind:    "CustomResourceDefinition",
	}
	if !reflect.DeepEqual(crd.GroupVersionKind(), gvk) {
		t.Fatalf("Expected: %s Got: %s", gvk, crd.GroupVersionKind())
	}
}

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
apiVersion: linkerd.io/v1alpha2
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
apiVersion: apiregistration.k8s.io/v1
kind: APIService
metadata:
  name: v1alpha1.tap.linkerd.io
  labels:
    linkerd.io/control-plane-component: tap
    linkerd.io/control-plane-ns: linkerd
spec:
  group: tap.linkerd.io
  version: v1alpha1
  groupPriorityMinimum: 1000
  versionPriority: 100
  service:
    name: linkerd-tap
    namespace: linkerd
  caBundle: dGFwIGNydA==`,
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
			_, _, _, _, _, err := NewFakeClientSets(tc.k8sConfigs...)
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
apiVersion: linkerd.io/v1alpha2
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
- apiVersion: linkerd.io/v1alpha2
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
apiVersion: apiregistration.k8s.io/v1
kind: APIService
metadata:
  name: v1alpha1.tap.linkerd.io
  labels:
    linkerd.io/control-plane-component: tap
    linkerd.io/control-plane-ns: linkerd
spec:
  group: tap.linkerd.io
  version: v1alpha1
  groupPriorityMinimum: 1000
  versionPriority: 100
  service:
    name: linkerd-tap
    namespace: linkerd
  caBundle: dGFwIGNydA==`,
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

			_, _, _, _, _, err := newFakeClientSetsFromManifests(readers)
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
apiVersion: linkerd.io/v1alpha2
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
			`
apiVersion: apiextensions.k8s.io/v1beta1
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
			nil,
		},
		{
			`
apiVersion: apiregistration.k8s.io/v1
kind: APIService
metadata:
  name: v1alpha1.tap.linkerd.io
  labels:
    linkerd.io/control-plane-component: tap
    linkerd.io/control-plane-ns: linkerd
spec:
  group: tap.linkerd.io
  version: v1alpha1
  groupPriorityMinimum: 1000
  versionPriority: 100
  service:
    name: linkerd-tap
    namespace: linkerd
  caBundle: dGFwIGNydA==`,
			nil,
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
