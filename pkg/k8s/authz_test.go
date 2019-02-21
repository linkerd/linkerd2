package k8s

import (
	"fmt"
	"testing"
)

func TestResourceAuthz(t *testing.T) {
	tests := []struct {
		k8sConfigs []string
		allowed    bool
		reason     string
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
- apiGroups: ["extensions", "apps"]
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
			false,
			"",
			nil,
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("%d: returns expected authorization", i), func(t *testing.T) {
			k8sClient, _ := NewFakeClientSets(test.k8sConfigs...)
			allowed, reason, err := ResourceAuthz(k8sClient, "", "list", "extensions", "v1beta1", "deployments")
			if err != nil || test.err != nil {
				if (err == nil && test.err != nil) ||
					(err != nil && test.err == nil) ||
					(err.Error() != test.err.Error()) {
					t.Fatalf("Unexpected error (Expected: %s, Got: %s)", test.err, err)
				}
			}
			if allowed != test.allowed {
				t.Errorf("Allowed mismatch. Expected %v, but got %v", test.allowed, allowed)
			}
			if reason != test.reason {
				t.Errorf("Reason mismatch. Expected %v, but got %v", test.reason, reason)
			}
		})
	}
}
