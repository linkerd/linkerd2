package k8s

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	policyv1alpha1 "github.com/linkerd/linkerd2/controller/gen/apis/policy/v1alpha1"
	serverv1beta3 "github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta3"
	l5dcrdfake "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestAuthorizationsForResourceTargets(t *testing.T) {
	testCases := []struct {
		name       string
		policies   []*policyv1alpha1.AuthorizationPolicy
		wantAuthzs []Authorization
		wantStderr string
	}{
		{
			name: "Namespace target with the core group is resolved",
			policies: []*policyv1alpha1.AuthorizationPolicy{
				authzPolicy("ns-authz", K8sCoreAPIGroup, NamespaceKind, "emojivoto"),
			},
			wantAuthzs: []Authorization{{Server: "web-http", AuthorizationPolicy: "ns-authz"}},
		},
		{
			name: "Namespace target with an empty group is resolved",
			policies: []*policyv1alpha1.AuthorizationPolicy{
				authzPolicy("ns-authz", "", NamespaceKind, "emojivoto"),
			},
			wantAuthzs: []Authorization{{Server: "web-http", AuthorizationPolicy: "ns-authz"}},
		},
		{
			name: "unsupported target is skipped with a warning",
			policies: []*policyv1alpha1.AuthorizationPolicy{
				authzPolicy("web-http-authz", PolicyAPIGroup, ServerKind, "web-http"),
				authzPolicy("gw-route-authz", "gateway.networking.k8s.io", HTTPRouteKind, "gw-route"),
			},
			wantAuthzs: []Authorization{{Server: "web-http", AuthorizationPolicy: "web-http-authz"}},
			wantStderr: "AuthorizationPolicy/gw-route-authz targets HTTPRoute.gateway.networking.k8s.io/gw-route which is not supported by this command; skipping\n",
		},
		{
			name: "unsupported target with an empty group is rendered without a trailing dot",
			policies: []*policyv1alpha1.AuthorizationPolicy{
				authzPolicy("sa-authz", "", "ServiceAccount", "web"),
			},
			wantStderr: "AuthorizationPolicy/sa-authz targets ServiceAccount/web which is not supported by this command; skipping\n",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			k8sAPI, err := NewFakeAPI(`
apiVersion: v1
kind: Pod
metadata:
  name: web
  namespace: emojivoto
  labels:
    app: web
spec:
  containers:
  - name: web
    ports:
    - containerPort: 8080
`)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			objs := []runtime.Object{
				&serverv1beta3.Server{
					ObjectMeta: metav1.ObjectMeta{Name: "web-http", Namespace: "emojivoto"},
					Spec: serverv1beta3.ServerSpec{
						PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
						Port:        intstr.FromInt(8080),
					},
				},
			}
			for _, p := range tc.policies {
				objs = append(objs, p)
			}
			k8sAPI.L5dCrdClient = l5dcrdfake.NewSimpleClientset(objs...)

			var authzs []Authorization
			stderr := captureStderr(t, func() {
				authzs, err = AuthorizationsForResource(context.Background(), k8sAPI, "emojivoto", "pod")
			})
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			if len(authzs) != len(tc.wantAuthzs) {
				t.Fatalf("expected %d authorizations, got %+v", len(tc.wantAuthzs), authzs)
			}
			for i, want := range tc.wantAuthzs {
				if authzs[i] != want {
					t.Errorf("expected authorization %+v, got %+v", want, authzs[i])
				}
			}

			if stderr != tc.wantStderr {
				t.Errorf("expected stderr %q, got %q", tc.wantStderr, stderr)
			}
		})
	}
}

func authzPolicy(name, group, kind, targetName string) *policyv1alpha1.AuthorizationPolicy {
	return &policyv1alpha1.AuthorizationPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "emojivoto"},
		Spec: policyv1alpha1.AuthorizationPolicySpec{
			TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
				Group: gatewayapiv1alpha2.Group(group),
				Kind:  gatewayapiv1alpha2.Kind(kind),
				Name:  gatewayapiv1alpha2.ObjectName(targetName),
			},
		},
	}
}

func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("error creating os.Pipe(): %s", err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	out := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out <- buf.String()
	}()

	f()
	w.Close()
	return <-out
}
