package k8s

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-test/deep"
)

func TestResourceAuthz(t *testing.T) {
	tests := []struct {
		k8sConfigs []string
		err        error
	}{
		{
			// TODO: determine the objects that will affect ResourceAuthz
			[]string{`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: cr-test
rules:
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["list"]`,
				`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: crb-test
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cr-test
subjects:
- kind: Group
  name: system:unauthenticated
  apiGroup: rbac.authorization.k8s.io`,
			},
			errors.New("not authorized to access deployments.apps"),
		},
	}

	ctx := context.Background()
	for i, test := range tests {
		test := test // pin
		t.Run(fmt.Sprintf("%d: returns expected authorization", i), func(t *testing.T) {
			k8sClient, err := NewFakeAPI(test.k8sConfigs...)
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}
			err = ResourceAuthz(ctx, k8sClient, "", "list", "apps", "v1", "deployments", "")
			if err != nil || test.err != nil {
				if (err == nil && test.err != nil) ||
					(err != nil && test.err == nil) ||
					(err.Error() != test.err.Error()) {
					t.Fatalf("Unexpected error (Expected: %s, Got: %s)", test.err, err)
				}
			}
		})
	}
}

func TestServiceProfilesAccess(t *testing.T) {
	fakeResources := []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: linkerd.io/v1alpha2
resources:
- name: serviceprofiles
  singularName: serviceprofile
  namespaced: true
  kind: ServiceProfile
  verbs:
  - delete
  - deletecollection
  - get
  - list
  - patch
  - create
  - update
  - watch
  shortNames:
  - sp`}

	api, err := NewFakeAPI(fakeResources...)
	if err != nil {
		t.Fatalf("NewFakeAPI error: %s", err)
	}

	err = ServiceProfilesAccess(context.Background(), api)
	// RBAC SSAR request failed, but the Discovery lookup succeeded
	if diff := deep.Equal(err, errors.New("not authorized to access serviceprofiles.linkerd.io")); diff != nil {
		t.Errorf("%+v", diff)
	}
}

func TestServersAccess(t *testing.T) {
	testCases := []struct {
		name          string
		resources     []string
		expectedError error
	}{
		{
			name: "supports version but not authorized",
			resources: []string{
				`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: servers.policy.linkerd.io
spec:
  conversion:
    strategy: None
  group: policy.linkerd.io
  names:
    kind: Server
    listKind: ServerList
    plural: servers
    shortNames:
    - srv
    singular: server
  scope: Namespaced
`, `
kind: APIResourceList
apiVersion: v1beta1
groupVersion: policy.linkerd.io/v1beta2
resources:
- name: servers
  singularName: server
  namespaced: true
  kind: Server
  verbs:
  - delete
  - deletecollection
  - get
  - list
  - patch
  - create
  - update
  - watch
  shortNames:
  - srv
`},
			expectedError: errors.New("not authorized to access servers.policy.linkerd.io"),
		},
		{
			name: "does not support version",
			resources: []string{
				`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: servers.policy.linkerd.io
spec:
  conversion:
    strategy: None
  group: policy.linkerd.io
  names:
    kind: Server
    listKind: ServerList
    plural: servers
    shortNames:
    - srv
    singular: server
  scope: Namespaced
`, `
kind: APIResourceList
apiVersion: v1beta1
groupVersion: policy.linkerd.io/v1beta1
resources:
- name: servers
  singularName: server
  namespaced: true
  kind: Server
  verbs:
  - delete
  - deletecollection
  - get
  - list
  - patch
  - create
  - update
  - watch
  shortNames:
  - srv
`},
			expectedError: errors.New(`the server could not find the requested resource, GroupVersion "policy.linkerd.io/v1beta2" not found`),
		},
	}

	for _, tc := range testCases {
		tc := tc // pin
		t.Run(tc.name, func(t *testing.T) {
			api, err := NewFakeAPI(tc.resources...)
			if err != nil {
				t.Errorf("unexpected error: %s", err)
			}

			err = ServersAccess(context.Background(), api.Interface)
			if err == nil {
				t.Fatal("Expected error, but got success")
			}
			if err.Error() != tc.expectedError.Error() {
				t.Fatalf("Unexpected error (Expected: %s, Got: %s)", tc.expectedError, err)
			}
		})
	}
}
