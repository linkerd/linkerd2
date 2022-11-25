package healthcheck

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-test/deep"
	"github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/identity"
	"github.com/linkerd/linkerd2/pkg/issuercerts"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type observer struct {
	results []string
}

func newObserver() *observer {
	return &observer{
		results: []string{},
	}
}
func (o *observer) resultFn(result *CheckResult) {
	res := fmt.Sprintf("%s %s", result.Category, result.Description)
	if result.Err != nil {
		res += fmt.Sprintf(": %s", result.Err)
	}
	o.results = append(o.results, res)
}

func (o *observer) resultWithHintFn(result *CheckResult) {
	res := fmt.Sprintf("%s %s", result.Category, result.Description)
	if result.Err != nil {
		res += fmt.Sprintf(": %s", result.Err)
	}

	if result.HintURL != "" {
		res += fmt.Sprintf(": %s", result.HintURL)
	}
	o.results = append(o.results, res)
}

func (hc *HealthChecker) addCheckAsCategory(
	testCategoryID CategoryID,
	categoryID CategoryID,
	desc string,
) {
	testCategory := NewCategory(
		testCategoryID,
		[]Checker{},
		false,
	)

	for _, cat := range hc.categories {
		if cat.ID == categoryID {
			for _, ch := range cat.checkers {
				if ch.description == desc {
					testCategory.checkers = append(testCategory.checkers, ch)
					testCategory.enabled = true
					break
				}
			}
			break
		}
	}
	hc.AppendCategories(testCategory)
}

func TestHealthChecker(t *testing.T) {
	nullObserver := func(*CheckResult) {}

	passingCheck1 := NewCategory(
		"cat1",
		[]Checker{
			{
				description: "desc1",
				check: func(context.Context) error {
					return nil
				},
				retryDeadline: time.Time{},
			},
		},
		true,
	)

	passingCheck2 := NewCategory(
		"cat2",
		[]Checker{
			{
				description: "desc2",
				check: func(context.Context) error {
					return nil
				},
				retryDeadline: time.Time{},
			},
		},
		true,
	)

	failingCheck := NewCategory(
		"cat3",
		[]Checker{
			{
				description: "desc3",
				check: func(context.Context) error {
					return fmt.Errorf("error")
				},
				retryDeadline: time.Time{},
			},
		},
		true,
	)

	fatalCheck := NewCategory(
		"cat6",
		[]Checker{
			{
				description: "desc6",
				fatal:       true,
				check: func(context.Context) error {
					return fmt.Errorf("fatal")
				},
				retryDeadline: time.Time{},
			},
		},
		true,
	)

	skippingCheck := NewCategory(
		"cat7",
		[]Checker{
			{
				description: "skip",
				check: func(context.Context) error {
					return SkipError{Reason: "needs skipping"}
				},
				retryDeadline: time.Time{},
			},
		},
		true,
	)

	skippingRPCCheck := NewCategory(
		"cat8",
		[]Checker{
			{
				description: "skipRpc",
				check: func(context.Context) error {
					return SkipError{Reason: "needs skipping"}
				},
				retryDeadline: time.Time{},
			},
		},
		true,
	)

	troubleshootingCheck := NewCategory(
		"cat9",
		[]Checker{
			{
				description: "failCheck",
				hintAnchor:  "cat9",
				check: func(context.Context) error {
					return fmt.Errorf("fatal")
				},
			},
		},
		true,
	)

	t.Run("Notifies observer of all results", func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)

		hc.AppendCategories(passingCheck1)
		hc.AppendCategories(passingCheck2)
		hc.AppendCategories(failingCheck)

		expectedResults := []string{
			"cat1 desc1",
			"cat2 desc2",
			"cat3 desc3: error",
		}

		obs := newObserver()
		hc.RunChecks(obs.resultFn)

		if diff := deep.Equal(obs.results, expectedResults); diff != nil {
			t.Fatalf("%+v", diff)
		}
	})

	t.Run("Is successful if all checks were successful", func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.AppendCategories(passingCheck1)
		hc.AppendCategories(passingCheck2)

		success, _ := hc.RunChecks(nullObserver)

		if !success {
			t.Fatalf("Expecting checks to be successful, but got [%t]", success)
		}
	})

	t.Run("Is not successful if one check fails", func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.AppendCategories(passingCheck1)
		hc.AppendCategories(failingCheck)
		hc.AppendCategories(passingCheck2)

		success, _ := hc.RunChecks(nullObserver)

		if success {
			t.Fatalf("Expecting checks to not be successful, but got [%t]", success)
		}
	})

	t.Run("Check for troubleshooting URL", func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		troubleshootingCheck.WithHintBaseURL("www.extension.com/troubleshooting/#")
		hc.AppendCategories(troubleshootingCheck)
		expectedResults := []string{
			"cat9 failCheck: fatal: www.extension.com/troubleshooting/#cat9",
		}

		obs := newObserver()
		hc.RunChecks(obs.resultWithHintFn)

		if diff := deep.Equal(obs.results, expectedResults); diff != nil {
			t.Fatalf("%+v", diff)
		}
	})

	t.Run("Does not run remaining check if fatal check fails", func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.AppendCategories(passingCheck1)
		hc.AppendCategories(fatalCheck)
		hc.AppendCategories(passingCheck2)

		expectedResults := []string{
			"cat1 desc1",
			"cat6 desc6: fatal",
		}

		obs := newObserver()
		hc.RunChecks(obs.resultFn)

		if diff := deep.Equal(obs.results, expectedResults); diff != nil {
			t.Fatalf("%+v", diff)
		}
	})

	t.Run("Retries checks if retry is specified", func(t *testing.T) {
		retryWindow = 0
		returnError := true

		retryCheck := NewCategory(
			"cat7",
			[]Checker{
				{
					description:   "desc7",
					retryDeadline: time.Now().Add(100 * time.Second),
					check: func(context.Context) error {
						if returnError {
							returnError = false
							return fmt.Errorf("retry")
						}
						return nil
					},
				},
			},
			true,
		)

		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.AppendCategories(passingCheck1)
		hc.AppendCategories(retryCheck)

		observedResults := make([]string, 0)
		observer := func(result *CheckResult) {
			res := fmt.Sprintf("%s %s retry=%t", result.Category, result.Description, result.Retry)
			if result.Err != nil {
				res += fmt.Sprintf(": %s", result.Err)
			}
			observedResults = append(observedResults, res)
		}

		expectedResults := []string{
			"cat1 desc1 retry=false",
			"cat7 desc7 retry=true: waiting for check to complete",
			"cat7 desc7 retry=false",
		}

		hc.RunChecks(observer)

		if diff := deep.Equal(observedResults, expectedResults); diff != nil {
			t.Fatalf("%+v", diff)
		}
	})

	t.Run("Does not notify observer of skipped checks", func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.AppendCategories(passingCheck1)
		hc.AppendCategories(skippingCheck)
		hc.AppendCategories(skippingRPCCheck)

		expectedResults := []string{
			"cat1 desc1",
		}

		obs := newObserver()
		hc.RunChecks(obs.resultFn)

		if diff := deep.Equal(obs.results, expectedResults); diff != nil {
			t.Fatalf("%+v", diff)
		}
	})
}

func TestCheckCanCreate(t *testing.T) {
	exp := fmt.Errorf("not authorized to access deployments.apps")

	hc := NewHealthChecker(
		[]CategoryID{},
		&Options{},
	)
	var err error
	hc.kubeAPI, err = k8s.NewFakeAPI()
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	err = hc.checkCanCreate(context.Background(), "", "apps", "v1", "deployments")
	if err == nil ||
		err.Error() != exp.Error() {
		t.Fatalf("Unexpected error (Expected: %s, Got: %s)", exp, err)
	}
}

func TestCheckExtensionAPIServerAuthentication(t *testing.T) {
	tests := []struct {
		k8sConfigs []string
		err        error
	}{
		{
			[]string{},
			fmt.Errorf("configmaps %q not found", k8s.ExtensionAPIServerAuthenticationConfigMapName),
		},
		{
			[]string{`
apiVersion: v1
kind: ConfigMap
metadata:
 name: extension-apiserver-authentication
 namespace: kube-system
data:
 foo : 'bar'
 `,
			},
			fmt.Errorf("--%s is not configured", k8s.ExtensionAPIServerAuthenticationRequestHeaderClientCAFileKey),
		},
		{

			[]string{fmt.Sprintf(`
apiVersion: v1
kind: ConfigMap
metadata:
 name: extension-apiserver-authentication
 namespace: kube-system
data:
  %s : 'bar'
  `, k8s.ExtensionAPIServerAuthenticationRequestHeaderClientCAFileKey)},
			nil,
		},
	}
	for i, test := range tests {
		test := test
		t.Run(fmt.Sprintf("%d: returns expected extension apiserver authentication check result", i), func(t *testing.T) {
			hc := NewHealthChecker([]CategoryID{}, &Options{})
			var err error
			hc.kubeAPI, err = k8s.NewFakeAPI(test.k8sConfigs...)
			if err != nil {
				t.Fatal(err)
			}
			err = hc.checkExtensionAPIServerAuthentication(context.Background())
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

func TestCheckClockSkew(t *testing.T) {
	tests := []struct {
		k8sConfigs []string
		err        error
	}{
		{
			[]string{},
			nil,
		},
		{
			[]string{`apiVersion: v1
kind: Node
metadata:
  name: test-node
status:
  conditions:
  - lastHeartbeatTime: "2000-01-01T01:00:00Z"
    status: "True"
    type: Ready`,
			},
			fmt.Errorf("clock skew detected for node(s): test-node"),
		},
	}

	for i, test := range tests {
		test := test // pin
		t.Run(fmt.Sprintf("%d: returns expected clock skew check result", i), func(t *testing.T) {
			hc := NewHealthChecker(
				[]CategoryID{},
				&Options{},
			)

			var err error
			hc.kubeAPI, err = k8s.NewFakeAPI(test.k8sConfigs...)
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}

			err = hc.checkClockSkew(context.Background())
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

func TestConfigExists(t *testing.T) {

	namespace := []string{`
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
`}
	clusterRoles := []string{`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-identity
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
		`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-proxy-injector
  labels:
    linkerd.io/control-plane-ns: test-ns
`}
	clusterRoleBindings := []string{`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-identity
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
		`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-proxy-injector
  labels:
    linkerd.io/control-plane-ns: test-ns
`}
	serviceAccounts := []string{`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-destination
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
		`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-identity
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
		`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-proxy-injector
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
		`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-heartbeat
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`}
	crds := []string{}
	for _, crd := range []struct{ name, version string }{
		{name: "authorizationpolicies.policy.linkerd.io", version: "v1alpha1"},
		{name: "meshtlsauthentications.policy.linkerd.io", version: "v1alpha1"},
		{name: "networkauthentications.policy.linkerd.io", version: "v1alpha1"},
		{name: "serverauthorizations.policy.linkerd.io", version: "v1beta1"},
		{name: "servers.policy.linkerd.io", version: "v1beta1"},
		{name: "serviceprofiles.linkerd.io", version: "v1alpha2"},
	} {
		crds = append(crds, fmt.Sprintf(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: %s
  labels:
    linkerd.io/control-plane-ns: test-ns
spec:
  versions:
  - name: %s`, crd.name, crd.version))
	}
	mutatingWebhooks := []string{`
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: linkerd-proxy-injector-webhook-config
  labels:
    linkerd.io/control-plane-ns: test-ns
`}
	validatingWebhooks := []string{`
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: linkerd-sp-validator-webhook-config
  labels:
    linkerd.io/control-plane-ns: test-ns
`}

	testCases := []struct {
		k8sConfigs []string
		results    []string
	}{
		{
			[]string{},
			[]string{"linkerd-config control plane Namespace exists: The \"test-ns\" namespace does not exist"},
		},
		{
			namespace,
			[]string{
				"linkerd-config control plane Namespace exists",
				"linkerd-config control plane ClusterRoles exist: missing ClusterRoles: linkerd-test-ns-identity, linkerd-test-ns-proxy-injector",
			},
		},
		{
			multiappend(
				namespace,
				clusterRoles,
			),
			[]string{
				"linkerd-config control plane Namespace exists",
				"linkerd-config control plane ClusterRoles exist",
				"linkerd-config control plane ClusterRoleBindings exist: missing ClusterRoleBindings: linkerd-test-ns-identity, linkerd-test-ns-proxy-injector",
			},
		},
		{
			multiappend(
				namespace,
				clusterRoles,
				clusterRoleBindings,
				serviceAccounts,
			),
			[]string{
				"linkerd-config control plane Namespace exists",
				"linkerd-config control plane ClusterRoles exist",
				"linkerd-config control plane ClusterRoleBindings exist",
				"linkerd-config control plane ServiceAccounts exist",
				"linkerd-config control plane CustomResourceDefinitions exist: missing authorizationpolicies.policy.linkerd.io, missing meshtlsauthentications.policy.linkerd.io, missing networkauthentications.policy.linkerd.io, missing serverauthorizations.policy.linkerd.io, missing servers.policy.linkerd.io, missing serviceprofiles.linkerd.io",
			},
		},
		{
			multiappend(
				namespace,
				clusterRoles,
				clusterRoleBindings,
				serviceAccounts,
				crds,
			),
			[]string{
				"linkerd-config control plane Namespace exists",
				"linkerd-config control plane ClusterRoles exist",
				"linkerd-config control plane ClusterRoleBindings exist",
				"linkerd-config control plane ServiceAccounts exist",
				"linkerd-config control plane CustomResourceDefinitions exist",
				"linkerd-config control plane MutatingWebhookConfigurations exist: missing MutatingWebhookConfigurations: linkerd-proxy-injector-webhook-config",
			},
		},
		{
			multiappend(
				namespace,
				clusterRoles,
				clusterRoleBindings,
				serviceAccounts,
				crds,
				mutatingWebhooks,
			),
			[]string{
				"linkerd-config control plane Namespace exists",
				"linkerd-config control plane ClusterRoles exist",
				"linkerd-config control plane ClusterRoleBindings exist",
				"linkerd-config control plane ServiceAccounts exist",
				"linkerd-config control plane CustomResourceDefinitions exist",
				"linkerd-config control plane MutatingWebhookConfigurations exist",
				"linkerd-config control plane ValidatingWebhookConfigurations exist: missing ValidatingWebhookConfigurations: linkerd-sp-validator-webhook-config",
			},
		},
		{
			multiappend(
				namespace,
				clusterRoles,
				clusterRoleBindings,
				serviceAccounts,
				crds,
				mutatingWebhooks,
				validatingWebhooks,
			),
			[]string{
				"linkerd-config control plane Namespace exists",
				"linkerd-config control plane ClusterRoles exist",
				"linkerd-config control plane ClusterRoleBindings exist",
				"linkerd-config control plane ServiceAccounts exist",
				"linkerd-config control plane CustomResourceDefinitions exist",
				"linkerd-config control plane MutatingWebhookConfigurations exist",
				"linkerd-config control plane ValidatingWebhookConfigurations exist",
			},
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: returns expected config result", i), func(t *testing.T) {
			hc := NewHealthChecker(
				[]CategoryID{LinkerdConfigChecks},
				&Options{
					ControlPlaneNamespace: "test-ns",
					CRDManifest:           strings.Join(crds, "\n---\n"),
				},
			)

			var err error
			hc.kubeAPI, err = k8s.NewFakeAPI(tc.k8sConfigs...)
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}

			obs := newObserver()
			hc.RunChecks(obs.resultFn)
			if diff := deep.Equal(obs.results, tc.results); diff != nil {
				t.Fatalf("%+v", diff)
			}
		})
	}
}

func TestCheckControlPlanePodExistence(t *testing.T) {
	var testCases = []struct {
		checkDescription string
		resources        []string
		expected         []string
	}{
		{
			checkDescription: "'linkerd-config' config map exists",
			resources: []string{`
apiVersion: v1
kind: ConfigMap
metadata:
  name: linkerd-config
  namespace: test-ns
data:
  values: "{}"
`,
			},
			expected: []string{
				"cat1 'linkerd-config' config map exists",
			},
		},
	}

	for id, testCase := range testCases {
		testCase := testCase
		t.Run(fmt.Sprintf("%d", id), func(t *testing.T) {
			hc := NewHealthChecker(
				[]CategoryID{},
				&Options{
					ControlPlaneNamespace: "test-ns",
				},
			)

			var err error
			hc.kubeAPI, err = k8s.NewFakeAPI(testCase.resources...)
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}

			// validate that this check relies on the k8s api, not on hc.controlPlanePods
			hc.addCheckAsCategory("cat1", LinkerdControlPlaneExistenceChecks,
				testCase.checkDescription)

			obs := newObserver()
			hc.RunChecks(obs.resultFn)
			if diff := deep.Equal(obs.results, testCase.expected); diff != nil {
				t.Fatalf("%+v", diff)
			}
		})
	}
}

func TestCheckClusterNetworks(t *testing.T) {
	var testCases = []struct {
		checkDescription string
		k8sConfigs       []string
		expected         []string
	}{
		{
			checkDescription: "cluster networks contains all node podCIDRs",
			k8sConfigs: []string{`
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
`,
				`
apiVersion: v1
kind: Node
metadata:
  name: linkerd-test-ns-identity
spec:
  podCIDR: 90.10.90.24/24
`,
				`
apiVersion: v1
kind: Node
metadata:
  name: linkerd-test-ns-identity2
spec:
  podCIDR: 242.3.64.0/25
`,
				`
kind: ConfigMap
apiVersion: v1
metadata:
  name: linkerd-config
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
data:
  values: |
    clusterNetworks: "10.0.0.0/8,100.64.0.0/10,172.16.0.0/12,192.168.0.0/16"
`,
			},
			expected: []string{
				"linkerd-existence cluster networks contains all node podCIDRs: node has podCIDR(s) [242.3.64.0/25 90.10.90.24/24] which are not contained in the Linkerd clusterNetworks.\n\tTry installing linkerd via --set clusterNetworks=\"242.3.64.0/25\\,90.10.90.24/24\"",
			},
		},
		{
			checkDescription: "cluster networks contains all node podCIDRs",
			k8sConfigs: []string{`
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
`,
				`
apiVersion: v1
kind: Node
metadata:
  name: linkerd-test-ns-identity
spec:
  podCIDR: 10.0.0.24/24
`,
				`
kind: ConfigMap
apiVersion: v1
metadata:
  name: linkerd-config
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
data:
  values: |
    clusterNetworks: "10.0.0.0/8,100.64.0.0/10,172.16.0.0/12,192.168.0.0/16"
`,
			},
			expected: []string{
				"linkerd-existence cluster networks contains all node podCIDRs",
			},
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: returns expected config result", i), func(t *testing.T) {
			hc := NewHealthChecker(
				[]CategoryID{},
				&Options{
					ControlPlaneNamespace: "test-ns",
				},
			)

			var err error
			hc.kubeAPI, err = k8s.NewFakeAPI(tc.k8sConfigs...)
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}

			obs := newObserver()
			hc.addCheckAsCategory("linkerd-existence", LinkerdControlPlaneExistenceChecks,
				tc.checkDescription)
			hc.RunChecks(obs.resultFn)
			if diff := deep.Equal(obs.results, tc.expected); diff != nil {
				t.Fatalf("%+v", diff)
			}
		})
	}
}

func proxiesWithCertificates(certificates ...string) []string {
	result := []string{}
	for i, certificate := range certificates {
		result = append(result, fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: pod-%d
  namespace: namespace-%d
  labels:
    %s: linkerd
spec:
  containers:
  - name: %s
    env:
    - name: %s
      value: %s
`, i, i, k8s.ControllerNSLabel, k8s.ProxyContainerName, identity.EnvTrustAnchors, certificate))
	}
	return result
}

func TestCheckDataPlaneProxiesCertificate(t *testing.T) {
	const currentCertificate = "current-certificate"
	const oldCertificate = "old-certificate"

	linkerdIdentityTrustRoots := fmt.Sprintf(`
kind: ConfigMap
apiVersion: v1
metadata:
  name: %s
data:
  ca-bundle.crt: %s

`, "linkerd-identity-trust-roots", currentCertificate)

	var testCases = []struct {
		checkDescription string
		resources        []string
		namespace        string
		expectedErr      error
	}{
		{
			checkDescription: "all proxies match CA certificate (all namespaces)",
			resources:        proxiesWithCertificates(currentCertificate, currentCertificate),
			namespace:        "",
			expectedErr:      nil,
		},
		{
			checkDescription: "some proxies match CA certificate (all namespaces)",
			resources:        proxiesWithCertificates(currentCertificate, oldCertificate),
			namespace:        "",
			expectedErr:      errors.New("Some pods do not have the current trust bundle and must be restarted:\n\t* namespace-1/pod-1"),
		},
		{
			checkDescription: "no proxies match CA certificate (all namespaces)",
			resources:        proxiesWithCertificates(oldCertificate, oldCertificate),
			namespace:        "",
			expectedErr:      errors.New("Some pods do not have the current trust bundle and must be restarted:\n\t* namespace-0/pod-0\n\t* namespace-1/pod-1"),
		},
		{
			checkDescription: "some proxies match CA certificate (match in target namespace)",
			resources:        proxiesWithCertificates(currentCertificate, oldCertificate),
			namespace:        "namespace-0",
			expectedErr:      nil,
		},
		{
			checkDescription: "some proxies match CA certificate (unmatch in target namespace)",
			resources:        proxiesWithCertificates(currentCertificate, oldCertificate),
			namespace:        "namespace-1",
			expectedErr:      errors.New("Some pods do not have the current trust bundle and must be restarted:\n\t* pod-1"),
		},
		{
			checkDescription: "no proxies match CA certificate (specific namespace)",
			resources:        proxiesWithCertificates(oldCertificate, oldCertificate),
			namespace:        "namespace-0",
			expectedErr:      errors.New("Some pods do not have the current trust bundle and must be restarted:\n\t* pod-0"),
		},
	}

	for id, testCase := range testCases {
		testCase := testCase
		t.Run(fmt.Sprintf("%d", id), func(t *testing.T) {
			hc := NewHealthChecker([]CategoryID{}, &Options{})
			hc.DataPlaneNamespace = testCase.namespace

			var err error
			hc.kubeAPI, err = k8s.NewFakeAPI(append(testCase.resources, linkerdIdentityTrustRoots)...)
			if err != nil {
				t.Fatalf("Unexpected error: %q", err)
			}

			err = hc.checkDataPlaneProxiesCertificate(context.Background())
			if diff := deep.Equal(err, testCase.expectedErr); diff != nil {
				t.Fatalf("%+v", diff)
			}
		})
	}
}

func TestValidateControlPlanePods(t *testing.T) {
	pod := func(name string, phase corev1.PodPhase, ready bool) corev1.Pod {
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: corev1.PodStatus{
				Phase: phase,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  strings.Split(name, "-")[1],
						Ready: ready,
					},
				},
			},
		}
	}

	t.Run("Returns an error if not all pods are running", func(t *testing.T) {
		pods := []corev1.Pod{
			pod("linkerd-destination-9849948665-37082", corev1.PodFailed, true),
			pod("linkerd-identity-6849948664-27982", corev1.PodFailed, true),
		}

		err := validateControlPlanePods(pods)
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "No running pods for \"linkerd-destination\"" {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns an error if not all containers are ready", func(t *testing.T) {
		pods := []corev1.Pod{
			pod("linkerd-identity-6849948664-27982", corev1.PodRunning, true),
			pod("linkerd-tap-6c878df6c8-2hmtd", corev1.PodRunning, true),
		}

		err := validateControlPlanePods(pods)
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
	})

	t.Run("Returns nil if all pods are running and all containers are ready", func(t *testing.T) {
		pods := []corev1.Pod{
			pod("linkerd-destination-9849948665-37082", corev1.PodRunning, true),
			pod("linkerd-identity-6849948664-27982", corev1.PodRunning, true),
			pod("linkerd-proxy-injector-5f79ff4844-", corev1.PodRunning, true),
		}

		err := validateControlPlanePods(pods)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})

	// This test is just for ensuring full coverage of the validateControlPlanePods function
	t.Run("Returns an error if all the controller pods are not ready", func(t *testing.T) {
		pods := []corev1.Pod{
			pod("linkerd-destination-9849948665-37082", corev1.PodRunning, false),
			pod("linkerd-identity-6849948664-27982", corev1.PodRunning, false),
			pod("linkerd-proxy-injector-5f79ff4844-", corev1.PodRunning, false),
		}

		err := validateControlPlanePods(pods)
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
	})

	t.Run("Returns nil if, HA mode, at least one pod of each control plane component is ready", func(t *testing.T) {
		pods := []corev1.Pod{
			pod("linkerd-destination-9843948665-48082", corev1.PodRunning, true),
			pod("linkerd-destination-9843948665-48083", corev1.PodRunning, false),
			pod("linkerd-destination-9843948665-48084", corev1.PodFailed, false),
			pod("linkerd-identity-6849948664-27982", corev1.PodRunning, true),
			pod("linkerd-identity-6849948664-27983", corev1.PodRunning, false),
			pod("linkerd-identity-6849948664-27984", corev1.PodFailed, false),
			pod("linkerd-proxy-injector-5f79ff4844-", corev1.PodRunning, true),
		}

		err := validateControlPlanePods(pods)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})

	t.Run("Returns nil if all linkerd pods are running and pod list includes non-linkerd pod", func(t *testing.T) {
		pods := []corev1.Pod{
			pod("linkerd-destination-9843948665-48082", corev1.PodRunning, true),
			pod("linkerd-identity-6849948664-27982", corev1.PodRunning, true),
			pod("linkerd-proxy-injector-5f79ff4844-", corev1.PodRunning, true),
			pod("hello-43c25d", corev1.PodRunning, true),
		}

		err := validateControlPlanePods(pods)
		if err != nil {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})
}

func TestValidateDataPlaneNamespace(t *testing.T) {
	testCases := []struct {
		ns     string
		result string
	}{
		{
			"",
			"data-plane-ns-test-cat data plane namespace exists",
		},
		{
			"bad-ns",
			"data-plane-ns-test-cat data plane namespace exists: The \"bad-ns\" namespace does not exist",
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d/%s", i, tc.ns), func(t *testing.T) {
			hc := NewHealthChecker(
				[]CategoryID{},
				&Options{
					DataPlaneNamespace: tc.ns,
				},
			)
			var err error
			hc.kubeAPI, err = k8s.NewFakeAPI()
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}

			// create a synthetic category that only includes the "data plane namespace exists" check
			hc.addCheckAsCategory("data-plane-ns-test-cat", LinkerdDataPlaneChecks, "data plane namespace exists")

			expectedResults := []string{
				tc.result,
			}
			obs := newObserver()
			hc.RunChecks(obs.resultFn)
			if diff := deep.Equal(obs.results, expectedResults); diff != nil {
				t.Fatalf("%+v", diff)
			}
		})
	}
}

func TestCheckDataPlanePods(t *testing.T) {

	t.Run("Returns an error if no inject pods were found", func(t *testing.T) {
		err := CheckPodsRunning([]corev1.Pod{}, "emojivoto")
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "no \"linkerd-proxy\" containers found in the \"emojivoto\" namespace" {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns an error if not all pods are running", func(t *testing.T) {
		pods := []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "emoji-d9c7866bb-7v74n"},
				Status: corev1.PodStatus{
					Phase: "Running",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: true,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "vote-bot-644b8cb6b4-g8nlr"},
				Status: corev1.PodStatus{
					Phase: "Running",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: true,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "voting-65b9fffd77-rlwsd"},
				Status: corev1.PodStatus{
					Phase: "Failed",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: false,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "web-6cfbccc48-5g8px"},
				Status: corev1.PodStatus{
					Phase: "Running",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: true,
						},
					},
				},
			},
		}

		err := CheckPodsRunning(pods, "emojivoto")
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "pod \"voting-65b9fffd77-rlwsd\" status is Failed" {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Does not return an error if the pod is Evicted", func(t *testing.T) {
		pods := []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "emoji-d9c7866bb-7v74n"},
				Status: corev1.PodStatus{
					Phase: "Evicted",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: true,
						},
					},
				},
			},
		}

		err := CheckPodsRunning(pods, "emojivoto")
		if err != nil {
			t.Fatalf("Expected no error, got %s", err)
		}
	})

	t.Run("Does not return an error if the pod is in Shutdown state", func(t *testing.T) {
		pods := []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "emoji-d9c7866bb-7v74n"},
				Status: corev1.PodStatus{
					Phase:  "Failed",
					Reason: "Shutdown",
				},
			},
		}

		err := CheckPodsRunning(pods, "emojivoto")
		if err != nil {
			t.Fatalf("Expected no error, got %s", err)
		}
	})

	t.Run("Does not return an error if the pod is in NodeShutdown state", func(t *testing.T) {
		pods := []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "emoji-d9c7866bb-7v74n"},
				Status: corev1.PodStatus{
					Phase:  "Failed",
					Reason: "NodeShutdown",
				},
			},
		}

		err := CheckPodsRunning(pods, "emojivoto")
		if err != nil {
			t.Fatalf("Expected no error, got %s", err)
		}
	})

	t.Run("Does not return an error if the pod is in Terminated state", func(t *testing.T) {
		pods := []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "emoji-d9c7866bb-7v74n"},
				Status: corev1.PodStatus{
					Phase:  "Failed",
					Reason: "Terminated",
				},
			},
		}

		err := CheckPodsRunning(pods, "emojivoto")
		if err != nil {
			t.Fatalf("Expected no error, got %s", err)
		}
	})

	t.Run("Returns an error if the proxy container is not ready", func(t *testing.T) {
		pods := []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "emoji-d9c7866bb-7v74n"},
				Status: corev1.PodStatus{
					Phase: "Running",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: true,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "vote-bot-644b8cb6b4-g8nlr"},
				Status: corev1.PodStatus{
					Phase: "Running",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: false,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "voting-65b9fffd77-rlwsd"},
				Status: corev1.PodStatus{
					Phase: "Running",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: false,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "web-6cfbccc48-5g8px"},
				Status: corev1.PodStatus{
					Phase: "Running",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: true,
						},
					},
				},
			},
		}

		err := CheckPodsRunning(pods, "emojivoto")
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "container \"linkerd-proxy\" in pod \"vote-bot-644b8cb6b4-g8nlr\" is not ready" {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns nil if all pods are running and all proxy containers are ready", func(t *testing.T) {
		pods := []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "emoji-d9c7866bb-7v74n"},
				Status: corev1.PodStatus{
					Phase: "Running",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: true,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "vote-bot-644b8cb6b4-g8nlr"},
				Status: corev1.PodStatus{
					Phase: "Running",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: true,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "voting-65b9fffd77-rlwsd"},
				Status: corev1.PodStatus{
					Phase: "Running",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: true,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "web-6cfbccc48-5g8px"},
				Status: corev1.PodStatus{
					Phase: "Running",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: true,
						},
					},
				},
			},
		}

		err := CheckPodsRunning(pods, "emojivoto")
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})

	// This test relates to https://github.com/linkerd/linkerd2/issues/6128
	t.Run("Returns nil if some pods are in the Succeeded phase and their proxies are no longer ready", func(t *testing.T) {
		pods := []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "emoji-d9c7866bb-7v74n"},
				Status: corev1.PodStatus{
					Phase:  "Succeeded",
					Reason: "Completed",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: false,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "vote-bot-644b8cb6b4-g8nlr"},
				Status: corev1.PodStatus{
					Phase: "Running",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: true,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "voting-65b9fffd77-rlwsd"},
				Status: corev1.PodStatus{
					Phase: "Running",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: true,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "web-6cfbccc48-5g8px"},
				Status: corev1.PodStatus{
					Phase:  "Succeeded",
					Reason: "Completed",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  k8s.ProxyContainerName,
							Ready: false,
						},
					},
				},
			},
		}

		err := CheckPodsRunning(pods, "emojivoto")
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})
}

func TestDataPlanePodLabels(t *testing.T) {

	t.Run("Returns nil if pod labels are ok", func(t *testing.T) {
		pods := []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "emoji-d9c7866bb-7v74n",
					Annotations: map[string]string{k8s.ProxyControlPortAnnotation: "3000"},
					Labels:      map[string]string{"app": "test"},
				},
			},
		}

		err := checkMisconfiguredPodsLabels(pods)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})

	t.Run("Returns error if any labels are misconfigured", func(t *testing.T) {
		for _, tc := range []struct {
			description      string
			pods             []corev1.Pod
			expectedErrorMsg string
		}{
			{
				description: "config as label",
				pods: []corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "emoji-d9c7866bb-7v74n",
							Labels: map[string]string{k8s.ProxyControlPortAnnotation: "3000"},
						},
					},
				},
				expectedErrorMsg: "Some labels on data plane pods should be annotations:\n\t* /emoji-d9c7866bb-7v74n\n\t\tconfig.linkerd.io/control-port",
			},
			{
				description: "alpha config as label",
				pods: []corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "emoji-d9c7866bb-7v74n",
							Labels: map[string]string{k8s.ProxyConfigAnnotationsPrefixAlpha + "/alpha-setting": "3000"},
						},
					},
				},
				expectedErrorMsg: "Some labels on data plane pods should be annotations:\n\t* /emoji-d9c7866bb-7v74n\n\t\tconfig.alpha.linkerd.io/alpha-setting",
			},
			{
				description: "inject annotation as label",
				pods: []corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "emoji-d9c7866bb-7v74n",
							Labels: map[string]string{k8s.ProxyInjectAnnotation: "enable"},
						},
					},
				},
				expectedErrorMsg: "Some labels on data plane pods should be annotations:\n\t* /emoji-d9c7866bb-7v74n\n\t\tlinkerd.io/inject",
			},
		} {
			tc := tc // pin
			t.Run(tc.description, func(t *testing.T) {
				err := checkMisconfiguredPodsLabels(tc.pods)

				if err == nil {
					t.Fatal("Expected error, got nothing")
				}

				if err.Error() != tc.expectedErrorMsg {
					t.Fatalf("Unexpected error message: %s", err.Error())
				}
			})
		}
	})
}

func TestServicesLabels(t *testing.T) {

	t.Run("Returns nil if service labels are ok", func(t *testing.T) {
		services := []corev1.Service{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "emoji-d9c7866bb-7v74n",
					Annotations: map[string]string{k8s.ProxyControlPortAnnotation: "3000"},
					Labels:      map[string]string{"app": "test", k8s.DefaultExportedServiceSelector: "true"},
				},
			},
		}

		err := checkMisconfiguredServiceLabels(services)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})

	t.Run("Returns error if service labels or annotation misconfigured", func(t *testing.T) {
		for _, tc := range []struct {
			description      string
			services         []corev1.Service
			expectedErrorMsg string
		}{
			{
				description: "config as label",
				services: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "emoji-d9c7866bb-7v74n",
							Labels: map[string]string{k8s.ProxyControlPortAnnotation: "3000"},
						},
					},
				},
				expectedErrorMsg: "Some labels on data plane services should be annotations:\n\t* /emoji-d9c7866bb-7v74n\n\t\tconfig.linkerd.io/control-port",
			},
			{
				description: "alpha config as label",
				services: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "emoji-d9c7866bb-7v74n",
							Labels: map[string]string{k8s.ProxyConfigAnnotationsPrefixAlpha + "/alpha-setting": "3000"},
						},
					},
				},
				expectedErrorMsg: "Some labels on data plane services should be annotations:\n\t* /emoji-d9c7866bb-7v74n\n\t\tconfig.alpha.linkerd.io/alpha-setting",
			},
		} {
			tc := tc // pin
			t.Run(tc.description, func(t *testing.T) {
				err := checkMisconfiguredServiceLabels(tc.services)
				if err == nil {
					t.Fatal("Expected error, got nothing")
				}
				if err.Error() != tc.expectedErrorMsg {
					t.Fatalf("Unexpected error message: %s", err.Error())
				}
			})
		}
	})
}

func TestServicesAnnotations(t *testing.T) {

	t.Run("Returns nil if service annotations are ok", func(t *testing.T) {
		services := []corev1.Service{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "emoji-d9c7866bb-7v74n",
					Annotations: map[string]string{k8s.ProxyControlPortAnnotation: "3000"},
					Labels:      map[string]string{"app": "test", k8s.DefaultExportedServiceSelector: "true"},
				},
			},
		}

		err := checkMisconfiguredServiceAnnotations(services)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})

	t.Run("Returns error if service annotations are misconfigured", func(t *testing.T) {
		for _, tc := range []struct {
			description      string
			services         []corev1.Service
			expectedErrorMsg string
		}{
			{
				description: "mirror as annotations",
				services: []corev1.Service{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:        "emoji-d9c7866bb-7v74n",
							Annotations: map[string]string{k8s.DefaultExportedServiceSelector: "true"},
						},
					},
				},
				expectedErrorMsg: "Some annotations on data plane services should be labels:\n\t* /emoji-d9c7866bb-7v74n\n\t\tmirror.linkerd.io/exported",
			},
		} {
			tc := tc // pin
			t.Run(tc.description, func(t *testing.T) {
				err := checkMisconfiguredServiceAnnotations(tc.services)
				if err == nil {
					t.Fatal("Expected error, got nothing")
				}
				if err.Error() != tc.expectedErrorMsg {
					t.Fatalf("Unexpected error message: %s", err.Error())
				}
			})
		}
	})
}

func getWebhookAndKubeSystemNamespace(nsLabel string, failurePolicy string) []string {
	return []string{fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  creationTimestamp: null
  labels:
    %s
  name: kube-system`, nsLabel),
		fmt.Sprintf(`
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: linkerd-proxy-injector-webhook-config
  labels:
    linkerd.io/control-plane-component: proxy-injector
    linkerd.io/control-plane-ns: linkerd
webhooks:
- name: linkerd-proxy-injector.linkerd.io
  namespaceSelector:
    matchExpressions:
    - key: config.linkerd.io/admission-webhooks
      operator: NotIn
      values:
      - disabled
  clientConfig:
    service:
      name: linkerd-proxy-injector
      namespace: linkerd
      path: "/"
    caBundle: cHJveHkgaW5qZWN0b3IgQ0EgYnVuZGxl
  failurePolicy: %s
  rules:
  - operations: [ "CREATE" ]
    apiGroups: [""]
    apiVersions: ["v1"]
    resources: ["pods"]
  sideEffects: None`, failurePolicy),
	}
}

func TestKubeSystemNamespaceInHA(t *testing.T) {
	testCases := []struct {
		testDescription string
		k8sConfigs      []string
		expectedOutput  string
	}{
		{
			"passes when webhook policy is Ignore is not enabled",
			getWebhookAndKubeSystemNamespace("", "Ignore"),
			"",
		},
		{
			"passes when webhook policy is Fail and namespace has required metadata",
			getWebhookAndKubeSystemNamespace("config.linkerd.io/admission-webhooks: disabled", "Fail"),
			"l5d-injection-disabled pod injection disabled on kube-system",
		},
		{
			"fails when webhook policy is Fail and admission hooks are enabled",
			getWebhookAndKubeSystemNamespace("config.linkerd.io/admission-webhooks: enabled", "Fail"),
			"l5d-injection-disabled pod injection disabled on kube-system: kube-system namespace needs to have the label config.linkerd.io/admission-webhooks: disabled if injector webhook failure policy is Fail",
		},
		{
			"fails when webhook policy is Fail and metadata is missing",
			getWebhookAndKubeSystemNamespace("", "Fail"),
			"l5d-injection-disabled pod injection disabled on kube-system: kube-system namespace needs to have the label config.linkerd.io/admission-webhooks: disabled if injector webhook failure policy is Fail",
		},
	}

	for _, tc := range testCases {
		tc := tc // pin
		t.Run(tc.testDescription, func(t *testing.T) {

			hc := NewHealthChecker([]CategoryID{}, &Options{})
			hc.ControlPlaneNamespace = "linkerd"

			hc.kubeAPI, _ = k8s.NewFakeAPI(tc.k8sConfigs...)
			hc.addCheckAsCategory("l5d-injection-disabled", LinkerdHAChecks, "pod injection disabled on kube-system")

			obs := newObserver()
			hc.RunChecks(obs.resultFn)

			if tc.expectedOutput == "" {
				if len(obs.results) != 0 {
					t.Fatalf("Expected not output, but got %v", obs.results)
				}
			} else {
				expectedResults := []string{
					tc.expectedOutput,
				}
				if diff := deep.Equal(obs.results, expectedResults); diff != nil {
					t.Fatalf("%+v", diff)
				}
			}
		})
	}

}

func TestFetchCurrentConfiguration(t *testing.T) {
	defaultValues, err := linkerd2.NewValues()

	if err != nil {
		t.Fatalf("Unexpected error validating options: %v", err)
	}

	testCases := []struct {
		k8sConfigs []string
		expected   *linkerd2.Values
		err        error
	}{
		{
			[]string{`
kind: ConfigMap
apiVersion: v1
metadata:
  name: linkerd-config
  namespace: linkerd
data:
  global: |
    {"linkerdNamespace":"linkerd","cniEnabled":false,"version":"install-control-plane-version","identityContext":{"trustDomain":"cluster.local","trustAnchorsPem":"fake-trust-anchors-pem","issuanceLifetime":"86400s","clockSkewAllowance":"20s"}}
  proxy: |
    {"proxyImage":{"imageName":"cr.l5d.io/linkerd/proxy","pullPolicy":"IfNotPresent"},"proxyInitImage":{"imageName":"cr.l5d.io/linkerd/proxy-init","pullPolicy":"IfNotPresent"},"controlPort":{"port":4190},"ignoreInboundPorts":[],"ignoreOutboundPorts":[],"inboundPort":{"port":4143},"adminPort":{"port":4191},"outboundPort":{"port":4140},"resource":{"requestCpu":"","requestMemory":"","limitCpu":"","limitMemory":""},"proxyUid":"2102","logLevel":{"level":"warn,linkerd=info"},"disableExternalProfiles":true,"proxyVersion":"install-proxy-version","proxy_init_image_version":"v2.1.0","debugImage":{"imageName":"cr.l5d.io/linkerd/debug","pullPolicy":"IfNotPresent"},"debugImageVersion":"install-debug-version"}
  install: |
    {"cliVersion":"dev-undefined","flags":[]}
  values: |
    controllerImage: ControllerImage
    controllerReplicas: 1
    controllerUID: 2103
    debugContainer: null
    destinationProxyResources: null
    destinationResources: null
    disableHeartBeat: false
    enableH2Upgrade: true
    enablePodAntiAffinity: false
    nodeAffinity: null
    cliVersion: CliVersion
    clusterDomain: cluster.local
    clusterNetworks: ClusterNetworks
    cniEnabled: false
    controlPlaneTracing: false
    controllerLogLevel: ControllerLogLevel
    enableEndpointSlices: false
    highAvailability: false
    imagePullPolicy: ImagePullPolicy
    imagePullSecrets: null
    linkerdVersion: ""
    prometheusUrl: ""
    proxy:
      capabilities: null
      component: linkerd-controller
      disableTap: false
      enableExternalProfiles: false
      image:
        name: ProxyImageName
        pullPolicy: ImagePullPolicy
        version: ProxyVersion
      inboundConnectTimeout: ""
      isGateway: false
      logFormat: plain
      logLevel: warn,linkerd=info
      opaquePorts: ""
      outboundConnectTimeout: ""
      ports:
        admin: 4191
        control: 4190
        inbound: 4143
        outbound: 4140
      requireIdentityOnInboundPorts: ""
      resources: null
      saMountPath: null
      uid: 2102
      waitBeforeExitSeconds: 0
      workloadKind: deployment
    proxyContainerName: ProxyContainerName
    proxyInit:
      capabilities: null
      closeWaitTimeoutSecs: 0
      ignoreInboundPorts: ""
      ignoreOutboundPorts: ""
      image:
        name: ProxyInitImageName
        pullPolicy: ImagePullPolicy
        version: ProxyInitVersion
      resources:
        cpu:
          limit: 100m
          request: 10m
        memory:
          limit: 50Mi
          request: 10Mi
      saMountPath: null
      xtMountPath:
        mountPath: /run
        name: linkerd-proxy-init-xtables-lock
        readOnly: false
    heartbeatResources: null
    heartbeatSchedule: ""
    identityProxyResources: null
    identityResources: null
    nodeSelector:
      kubernetes.io/os: linux
    proxyInjectorProxyResources: null
    proxyInjectorResources: null
    stage: ""
    tolerations: null
    webhookFailurePolicy: WebhookFailurePolicy
`,
			},
			&linkerd2.Values{
				ControllerImage:      "ControllerImage",
				ControllerUID:        2103,
				EnableH2Upgrade:      true,
				WebhookFailurePolicy: "WebhookFailurePolicy",
				NodeSelector:         defaultValues.NodeSelector,
				Tolerations:          defaultValues.Tolerations,
				ClusterDomain:        "cluster.local",
				ClusterNetworks:      "ClusterNetworks",
				ImagePullPolicy:      "ImagePullPolicy",
				CliVersion:           "CliVersion",
				ControllerLogLevel:   "ControllerLogLevel",
				ProxyContainerName:   "ProxyContainerName",
				CNIEnabled:           false,
				Proxy: &linkerd2.Proxy{
					Image: &linkerd2.Image{
						Name:       "ProxyImageName",
						PullPolicy: "ImagePullPolicy",
						Version:    "ProxyVersion",
					},
					LogLevel:  "warn,linkerd=info",
					LogFormat: "plain",
					Ports: &linkerd2.Ports{
						Admin:    4191,
						Control:  4190,
						Inbound:  4143,
						Outbound: 4140,
					},
					UID: 2102,
				},
				ProxyInit: &linkerd2.ProxyInit{
					Image: &linkerd2.Image{
						Name:       "ProxyInitImageName",
						PullPolicy: "ImagePullPolicy",
						Version:    "ProxyInitVersion",
					},
					Resources: &linkerd2.Resources{
						CPU: linkerd2.Constraints{
							Limit:   "100m",
							Request: "10m",
						},
						Memory: linkerd2.Constraints{
							Limit:   "50Mi",
							Request: "10Mi",
						},
					},
					XTMountPath: &linkerd2.VolumeMountPath{
						MountPath: "/run",
						Name:      "linkerd-proxy-init-xtables-lock",
					},
				},
				ControllerReplicas: 1,
			},
			nil,
		},
		{
			[]string{`
kind: ConfigMap
apiVersion: v1
metadata:
  name: linkerd-config
  namespace: linkerd
data:
  global: |
    {"linkerdNamespace":"linkerd","cniEnabled":false,"version":"install-control-plane-version","identityContext":{"trustDomain":"cluster.local","trustAnchorsPem":"fake-trust-anchors-pem","issuanceLifetime":"86400s","clockSkewAllowance":"20s"}}
  proxy: |
    {"proxyImage":{"imageName":"cr.l5d.io/linkerd/proxy","pullPolicy":"IfNotPresent"},"proxyInitImage":{"imageName":"cr.l5d.io/linkerd/proxy-init","pullPolicy":"IfNotPresent"},"controlPort":{"port":4190},"ignoreInboundPorts":[],"ignoreOutboundPorts":[],"inboundPort":{"port":4143},"adminPort":{"port":4191},"outboundPort":{"port":4140},"resource":{"requestCpu":"","requestMemory":"","limitCpu":"","limitMemory":""},"proxyUid":"2102","logLevel":{"level":"warn,linkerd=info"},"disableExternalProfiles":true,"proxyVersion":"install-proxy-version","proxy_init_image_version":"v2.1.0","debugImage":{"imageName":"cr.l5d.io/linkerd/debug","pullPolicy":"IfNotPresent"},"debugImageVersion":"install-debug-version"}
  install: |
    {"cliVersion":"dev-undefined","flags":[]}
  values: |
    controllerImage: ControllerImage
    controllerReplicas: 1
    controllerUID: 2103
    debugContainer: null
    destinationProxyResources: null
    destinationResources: null
    disableHeartBeat: false
    enableH2Upgrade: true
    enablePodAntiAffinity: false
    global:
      cliVersion: CliVersion
      clusterDomain: cluster.local
      clusterNetworks: ClusterNetworks
      cniEnabled: false
      controlPlaneTracing: false
      controllerLogLevel: ControllerLogLevel
      enableEndpointSlices: false
      highAvailability: false
      imagePullPolicy: ImagePullPolicy
      imagePullSecrets: null
      linkerdVersion: ""
      prometheusUrl: ""
      proxy:
        capabilities: null
        component: linkerd-controller
        disableTap: false
        enableExternalProfiles: false
        image:
          name: ProxyImageName
          pullPolicy: ImagePullPolicy
          version: ProxyVersion
        inboundConnectTimeout: ""
        isGateway: false
        logFormat: plain
        logLevel: warn,linkerd=info
        opaquePorts: ""
        outboundConnectTimeout: ""
        ports:
          admin: 4191
          control: 4190
          inbound: 4143
          outbound: 4140
        requireIdentityOnInboundPorts: ""
        resources: null
        saMountPath: null
        uid: 2102
        waitBeforeExitSeconds: 0
        workloadKind: deployment
      proxyContainerName: ProxyContainerName
      proxyInit:
        capabilities: null
        closeWaitTimeoutSecs: 0
        ignoreInboundPorts: ""
        ignoreOutboundPorts: ""
        image:
          name: ProxyInitImageName
          pullPolicy: ImagePullPolicy
          version: ProxyInitVersion
        resources:
          cpu:
            limit: 100m
            request: 10m
          memory:
            limit: 50Mi
            request: 10Mi
        saMountPath: null
        xtMountPath:
          mountPath: /run
          name: linkerd-proxy-init-xtables-lock
          readOnly: false
    heartbeatResources: null
    heartbeatSchedule: ""
    identityProxyResources: null
    identityResources: null
    nodeSelector:
      kubernetes.io/os: linux
    proxyInjectorProxyResources: null
    proxyInjectorResources: null
    stage: ""
    tolerations: null
    webhookFailurePolicy: WebhookFailurePolicy
`,
			},
			&linkerd2.Values{
				ControllerImage:      "ControllerImage",
				ControllerUID:        2103,
				EnableH2Upgrade:      true,
				WebhookFailurePolicy: "WebhookFailurePolicy",
				NodeSelector:         defaultValues.NodeSelector,
				Tolerations:          defaultValues.Tolerations,
				ClusterDomain:        "cluster.local",
				ClusterNetworks:      "ClusterNetworks",
				ImagePullPolicy:      "ImagePullPolicy",
				CliVersion:           "CliVersion",
				ControllerLogLevel:   "ControllerLogLevel",
				ProxyContainerName:   "ProxyContainerName",
				CNIEnabled:           false,
				Proxy: &linkerd2.Proxy{
					Image: &linkerd2.Image{
						Name:       "ProxyImageName",
						PullPolicy: "ImagePullPolicy",
						Version:    "ProxyVersion",
					},
					LogLevel:  "warn,linkerd=info",
					LogFormat: "plain",
					Ports: &linkerd2.Ports{
						Admin:    4191,
						Control:  4190,
						Inbound:  4143,
						Outbound: 4140,
					},
					UID: 2102,
				},
				ProxyInit: &linkerd2.ProxyInit{
					Image: &linkerd2.Image{
						Name:       "ProxyInitImageName",
						PullPolicy: "ImagePullPolicy",
						Version:    "ProxyInitVersion",
					},
					Resources: &linkerd2.Resources{
						CPU: linkerd2.Constraints{
							Limit:   "100m",
							Request: "10m",
						},
						Memory: linkerd2.Constraints{
							Limit:   "50Mi",
							Request: "10Mi",
						},
					},
					XTMountPath: &linkerd2.VolumeMountPath{
						MountPath: "/run",
						Name:      "linkerd-proxy-init-xtables-lock",
					},
				},
				ControllerReplicas: 1,
			},
			nil,
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			clientset, err := k8s.NewFakeAPI(tc.k8sConfigs...)
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}

			_, values, err := FetchCurrentConfiguration(context.Background(), clientset, "linkerd")
			if diff := deep.Equal(err, tc.err); diff != nil {
				t.Fatalf("%+v", diff)
			}
			if diff := deep.Equal(values, tc.expected); diff != nil {
				t.Fatalf("%+v", diff)
			}
		})
	}
}

func getFakeConfigMap(scheme string, issuerCerts *issuercerts.IssuerCertData) string {
	anchors, _ := json.Marshal(issuerCerts.TrustAnchors)
	return fmt.Sprintf(`
kind: ConfigMap
apiVersion: v1
metadata:
  name: linkerd-config
  namespace: linkerd
data:
  values: |
    namespace: linkerd
    identityTrustAnchorsPEM: %s
    identityTrustDomain: cluster.local
    identity:
      issuer:
        scheme: %s
---
`, anchors, scheme)
}

func getFakeSecret(scheme string, issuerCerts *issuercerts.IssuerCertData) string {
	if scheme == k8s.IdentityIssuerSchemeLinkerd {
		return fmt.Sprintf(`
kind: Secret
apiVersion: v1
metadata:
  name: linkerd-identity-issuer
  namespace: linkerd
data:
  crt.pem: %s
  key.pem: %s
---
`, base64.StdEncoding.EncodeToString([]byte(issuerCerts.IssuerCrt)), base64.StdEncoding.EncodeToString([]byte(issuerCerts.IssuerKey)))
	}
	return fmt.Sprintf(
		`
kind: Secret
apiVersion: v1
metadata:
  name: linkerd-identity-issuer
  namespace: linkerd
data:
  ca.crt: %s
  tls.crt: %s
  tls.key: %s
---
`, base64.StdEncoding.EncodeToString([]byte(issuerCerts.TrustAnchors)), base64.StdEncoding.EncodeToString([]byte(issuerCerts.IssuerCrt)), base64.StdEncoding.EncodeToString([]byte(issuerCerts.IssuerKey)))
}

func createIssuerData(dnsName string, notBefore, notAfter time.Time) *issuercerts.IssuerCertData {
	// Generate a new root key.
	key, _ := tls.GenerateKey()

	rootCa, _ := tls.CreateRootCA(dnsName, key, tls.Validity{
		Lifetime:  notAfter.Sub(notBefore),
		ValidFrom: &notBefore,
	})

	return &issuercerts.IssuerCertData{
		TrustAnchors: rootCa.Cred.Crt.EncodeCertificatePEM(),
		IssuerCrt:    rootCa.Cred.Crt.EncodeCertificatePEM(),
		IssuerKey:    rootCa.Cred.EncodePrivateKeyPEM(),
	}
}

type lifeSpan struct {
	starts time.Time
	ends   time.Time
}

func runIdentityCheckTestCase(ctx context.Context, t *testing.T, testID int, testDescription string, checkerToTest string, fakeConfigMap string, fakeSecret string, expectedOutput []string) {
	t.Run(fmt.Sprintf("%d/%s", testID, testDescription), func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{
				DataPlaneNamespace: "linkerd",
			},
		)
		hc.addCheckAsCategory("linkerd-identity-test-cat", LinkerdIdentity, checkerToTest)
		var err error
		hc.ControlPlaneNamespace = "linkerd"
		hc.kubeAPI, err = k8s.NewFakeAPI(fakeConfigMap, fakeSecret)
		_, hc.linkerdConfig, _ = hc.checkLinkerdConfigConfigMap(ctx)

		if testDescription != "certificate config is valid" {
			hc.issuerCert, hc.trustAnchors, _ = hc.checkCertificatesConfig(ctx)
		}

		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		obs := newObserver()
		hc.RunChecks(obs.resultFn)
		if diff := deep.Equal(obs.results, expectedOutput); diff != nil {
			t.Fatalf("%+v", diff)
		}
	})
}

func TestLinkerdIdentityCheckCertConfig(t *testing.T) {
	var testCases = []struct {
		checkDescription            string
		tlsSecretScheme             string
		schemeInConfig              string
		expectedOutput              []string
		configMapIssuerDataModifier func(issuercerts.IssuerCertData) issuercerts.IssuerCertData
		tlsSecretIssuerDataModifier func(issuercerts.IssuerCertData) issuercerts.IssuerCertData
	}{
		{
			checkDescription: "works with valid cert and linkerd.io/tls secret",
			tlsSecretScheme:  k8s.IdentityIssuerSchemeLinkerd,
			schemeInConfig:   k8s.IdentityIssuerSchemeLinkerd,
			expectedOutput:   []string{"linkerd-identity-test-cat certificate config is valid"},
		},
		{
			checkDescription: "works with valid cert and kubernetes.io/tls secret",
			tlsSecretScheme:  string(corev1.SecretTypeTLS),
			schemeInConfig:   string(corev1.SecretTypeTLS),
			expectedOutput:   []string{"linkerd-identity-test-cat certificate config is valid"},
		},
		{
			checkDescription: "works if config scheme is empty and secret scheme is linkerd.io/tls (pre 2.7)",
			tlsSecretScheme:  k8s.IdentityIssuerSchemeLinkerd,
			schemeInConfig:   "",
			expectedOutput:   []string{"linkerd-identity-test-cat certificate config is valid"},
		},
		{
			checkDescription: "fails if config scheme is empty and secret scheme is kubernetes.io/tls (pre 2.7)",
			tlsSecretScheme:  string(corev1.SecretTypeTLS),
			schemeInConfig:   "",
			expectedOutput:   []string{"linkerd-identity-test-cat certificate config is valid: key crt.pem containing the issuer certificate needs to exist in secret linkerd-identity-issuer if --identity-external-issuer=false"},
		},
		{
			checkDescription: "fails when config scheme is linkerd.io/tls but secret scheme is kubernetes.io/tls in config is different than the one in the issuer secret",
			tlsSecretScheme:  string(corev1.SecretTypeTLS),
			schemeInConfig:   k8s.IdentityIssuerSchemeLinkerd,
			expectedOutput:   []string{"linkerd-identity-test-cat certificate config is valid: key crt.pem containing the issuer certificate needs to exist in secret linkerd-identity-issuer if --identity-external-issuer=false"},
		},
		{
			checkDescription: "fails when config scheme is kubernetes.io/tls but secret scheme is linkerd.io/tls in config is different than the one in the issuer secret",
			tlsSecretScheme:  k8s.IdentityIssuerSchemeLinkerd,
			schemeInConfig:   string(corev1.SecretTypeTLS),
			expectedOutput:   []string{"linkerd-identity-test-cat certificate config is valid: key ca.crt containing the trust anchors needs to exist in secret linkerd-identity-issuer if --identity-external-issuer=true"},
		},
		{
			checkDescription: "fails when trying to parse trust anchors from secret (extra newline in secret)",
			tlsSecretScheme:  string(corev1.SecretTypeTLS),
			schemeInConfig:   string(corev1.SecretTypeTLS),
			expectedOutput:   []string{"linkerd-identity-test-cat certificate config is valid: not a PEM certificate"},
			tlsSecretIssuerDataModifier: func(issuerData issuercerts.IssuerCertData) issuercerts.IssuerCertData {
				issuerData.TrustAnchors += "\n"
				return issuerData
			},
		},
	}

	for id, testCase := range testCases {
		testCase := testCase
		issuerData := createIssuerData("identity.linkerd.cluster.local", time.Now().AddDate(-1, 0, 0), time.Now().AddDate(1, 0, 0))
		var fakeConfigMap string
		if testCase.configMapIssuerDataModifier != nil {
			modifiedIssuerData := testCase.configMapIssuerDataModifier(*issuerData)
			fakeConfigMap = getFakeConfigMap(testCase.schemeInConfig, &modifiedIssuerData)
		} else {
			fakeConfigMap = getFakeConfigMap(testCase.schemeInConfig, issuerData)
		}

		var fakeSecret string
		if testCase.tlsSecretIssuerDataModifier != nil {
			modifiedIssuerData := testCase.tlsSecretIssuerDataModifier(*issuerData)
			fakeSecret = getFakeSecret(testCase.tlsSecretScheme, &modifiedIssuerData)
		} else {
			fakeSecret = getFakeSecret(testCase.tlsSecretScheme, issuerData)
		}
		runIdentityCheckTestCase(context.Background(), t, id, testCase.checkDescription, "certificate config is valid", fakeConfigMap, fakeSecret, testCase.expectedOutput)
	}
}

func TestLinkerdIdentityCheckCertValidity(t *testing.T) {
	var testCases = []struct {
		checkDescription string
		checkerToTest    string
		lifespan         *lifeSpan
		expectedOutput   []string
	}{
		{
			checkerToTest:    "trust anchors are within their validity period",
			checkDescription: "fails when the only anchor is not valid yet",
			lifespan: &lifeSpan{
				starts: time.Date(2100, 1, 1, 1, 1, 1, 1, time.UTC),
				ends:   time.Date(2101, 1, 1, 1, 1, 1, 1, time.UTC),
			},
			expectedOutput: []string{"linkerd-identity-test-cat trust anchors are within their validity period: Invalid anchors:\n\t* 1 identity.linkerd.cluster.local not valid before: 2100-01-01T01:00:51Z"},
		},
		{
			checkerToTest:    "trust anchors are within their validity period",
			checkDescription: "fails when the only trust anchor is expired",
			lifespan: &lifeSpan{
				starts: time.Date(1989, 1, 1, 1, 1, 1, 1, time.UTC),
				ends:   time.Date(1990, 1, 1, 1, 1, 1, 1, time.UTC),
			},
			expectedOutput: []string{"linkerd-identity-test-cat trust anchors are within their validity period: Invalid anchors:\n\t* 1 identity.linkerd.cluster.local not valid anymore. Expired on 1990-01-01T01:01:11Z"},
		},
		{
			checkerToTest:    "issuer cert is within its validity period",
			checkDescription: "fails when the issuer cert is not valid yet",
			lifespan: &lifeSpan{
				starts: time.Date(2100, 1, 1, 1, 1, 1, 1, time.UTC),
				ends:   time.Date(2101, 1, 1, 1, 1, 1, 1, time.UTC),
			},
			expectedOutput: []string{"linkerd-identity-test-cat issuer cert is within its validity period: issuer certificate is not valid before: 2100-01-01T01:00:51Z"},
		},
		{
			checkerToTest:    "issuer cert is within its validity period",
			checkDescription: "fails when the issuer cert is expired",
			lifespan: &lifeSpan{
				starts: time.Date(1989, 1, 1, 1, 1, 1, 1, time.UTC),
				ends:   time.Date(1990, 1, 1, 1, 1, 1, 1, time.UTC),
			},
			expectedOutput: []string{"linkerd-identity-test-cat issuer cert is within its validity period: issuer certificate is not valid anymore. Expired on 1990-01-01T01:01:11Z"},
		},
	}

	for id, testCase := range testCases {
		testCase := testCase
		issuerData := createIssuerData("identity.linkerd.cluster.local", testCase.lifespan.starts, testCase.lifespan.ends)
		fakeConfigMap := getFakeConfigMap(k8s.IdentityIssuerSchemeLinkerd, issuerData)
		fakeSecret := getFakeSecret(k8s.IdentityIssuerSchemeLinkerd, issuerData)
		runIdentityCheckTestCase(context.Background(), t, id, testCase.checkDescription, testCase.checkerToTest, fakeConfigMap, fakeSecret, testCase.expectedOutput)
	}
}

type fakeCniResourcesOpts struct {
	hasConfigMap          bool
	hasClusterRole        bool
	hasClusterRoleBinding bool
	hasServiceAccount     bool
	hasDaemonSet          bool
	scheduled             int
	ready                 int
}

func getFakeCniResources(opts fakeCniResourcesOpts) []string {
	var resources []string

	if opts.hasConfigMap {
		resources = append(resources, `
kind: ConfigMap
apiVersion: v1
metadata:
  name: linkerd-cni-config
  namespace: test-ns
  labels:
    linkerd.io/cni-resource: "true"
data:
  dest_cni_net_dir: "/etc/cni/net.d"
---
`)
	}

	if opts.hasClusterRole {
		resources = append(resources, `
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-cni
  labels:
    linkerd.io/cni-resource: "true"
rules:
- apiGroups: [""]
  resources: ["pods", "nodes", "namespaces"]
  verbs: ["list", "get", "watch"]
---
`)
	}

	if opts.hasClusterRoleBinding {
		resources = append(resources, `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: linkerd-cni
  labels:
    linkerd.io/cni-resource: "true"
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: linkerd-cni
subjects:
- kind: ServiceAccount
  name: linkerd-cni
  namespace: test-ns
---
`)
	}

	if opts.hasServiceAccount {
		resources = append(resources, `
apiVersion: v1
kind: ServiceAccount
metadata:
  name: linkerd-cni
  namespace: test-ns
  labels:
    linkerd.io/cni-resource: "true"
---
`)
	}

	if opts.hasDaemonSet {
		resources = append(resources, fmt.Sprintf(`
kind: DaemonSet
apiVersion: apps/v1
metadata:
  name: linkerd-cni
  namespace: test-ns
  labels:
    k8s-app: linkerd-cni
    linkerd.io/cni-resource: "true"
  annotations:
    linkerd.io/created-by: linkerd/cli git-b4266c93
spec:
  selector:
    matchLabels:
      k8s-app: linkerd-cni
  updateStrategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1
  template:
    metadata:
      labels:
        k8s-app: linkerd-cni
      annotations:
        linkerd.io/created-by: linkerd/cli git-b4266c93
    spec:
      nodeSelector:
        kubernetes.io/os: linux
      hostNetwork: true
      serviceAccountName: linkerd-cni
      containers:
      - name: install-cni
        image: cr.l5d.io/linkerd/cni-plugin:git-b4266c93
        env:
        - name: DEST_CNI_NET_DIR
          valueFrom:
            configMapKeyRef:
              name: linkerd-cni-config
              key: dest_cni_net_dir
        - name: DEST_CNI_BIN_DIR
          valueFrom:
            configMapKeyRef:
              name: linkerd-cni-config
              key: dest_cni_bin_dir
        - name: CNI_NETWORK_CONFIG
          valueFrom:
            configMapKeyRef:
              name: linkerd-cni-config
              key: cni_network_config
        - name: SLEEP
          value: "true"
        lifecycle:
          preStop:
            exec:
              command: ["kill","-15","1"]
        volumeMounts:
        - mountPath: /host/opt/cni/bin
          name: cni-bin-dir
        - mountPath: /host/etc/cni/net.d
          name: cni-net-dir
      volumes:
      - name: cni-bin-dir
        hostPath:
          path: /opt/cni/bin
      - name: cni-net-dir
        hostPath:
          path: /etc/cni/net.d
status:
  desiredNumberScheduled: %d
  numberReady: %d
---
`, opts.scheduled, opts.ready))
	}

	return resources

}

func TestCniChecks(t *testing.T) {
	testCases := []struct {
		description  string
		testCaseOpts fakeCniResourcesOpts
		results      []string
	}{
		{
			"fails when there is no config map",
			fakeCniResourcesOpts{},
			[]string{"linkerd-cni-plugin cni plugin ConfigMap exists: configmaps \"linkerd-cni-config\" not found"},
		},
		{
			"fails then there is no ClusterRole",
			fakeCniResourcesOpts{hasConfigMap: true},
			[]string{
				"linkerd-cni-plugin cni plugin ConfigMap exists",
				"linkerd-cni-plugin cni plugin ClusterRole exists: missing ClusterRole: linkerd-cni"},
		},
		{
			"fails then there is no ClusterRoleBinding",
			fakeCniResourcesOpts{hasConfigMap: true, hasClusterRole: true},
			[]string{
				"linkerd-cni-plugin cni plugin ConfigMap exists",
				"linkerd-cni-plugin cni plugin ClusterRole exists",
				"linkerd-cni-plugin cni plugin ClusterRoleBinding exists: missing ClusterRoleBinding: linkerd-cni"},
		},
		{
			"fails then there is no ServiceAccount",
			fakeCniResourcesOpts{hasConfigMap: true, hasClusterRole: true, hasClusterRoleBinding: true},
			[]string{
				"linkerd-cni-plugin cni plugin ConfigMap exists",
				"linkerd-cni-plugin cni plugin ClusterRole exists",
				"linkerd-cni-plugin cni plugin ClusterRoleBinding exists",
				"linkerd-cni-plugin cni plugin ServiceAccount exists: missing ServiceAccount: linkerd-cni",
			},
		},
		{
			"fails then there is no DaemonSet",
			fakeCniResourcesOpts{hasConfigMap: true, hasClusterRole: true, hasClusterRoleBinding: true, hasServiceAccount: true},
			[]string{
				"linkerd-cni-plugin cni plugin ConfigMap exists",
				"linkerd-cni-plugin cni plugin ClusterRole exists",
				"linkerd-cni-plugin cni plugin ClusterRoleBinding exists",
				"linkerd-cni-plugin cni plugin ServiceAccount exists",
				"linkerd-cni-plugin cni plugin DaemonSet exists: missing DaemonSet: linkerd-cni",
			},
		},
		{
			"fails then there is nodes are not ready",
			fakeCniResourcesOpts{hasConfigMap: true, hasClusterRole: true, hasClusterRoleBinding: true, hasServiceAccount: true, hasDaemonSet: true, scheduled: 5, ready: 4},
			[]string{
				"linkerd-cni-plugin cni plugin ConfigMap exists",
				"linkerd-cni-plugin cni plugin ClusterRole exists",
				"linkerd-cni-plugin cni plugin ClusterRoleBinding exists",
				"linkerd-cni-plugin cni plugin ServiceAccount exists",
				"linkerd-cni-plugin cni plugin DaemonSet exists",
				"linkerd-cni-plugin cni plugin pod is running on all nodes: number ready: 4, number scheduled: 5",
			},
		},
		{
			"fails then there is nodes are not ready",
			fakeCniResourcesOpts{hasConfigMap: true, hasClusterRole: true, hasClusterRoleBinding: true, hasServiceAccount: true, hasDaemonSet: true, scheduled: 5, ready: 5},
			[]string{
				"linkerd-cni-plugin cni plugin ConfigMap exists",
				"linkerd-cni-plugin cni plugin ClusterRole exists",
				"linkerd-cni-plugin cni plugin ClusterRoleBinding exists",
				"linkerd-cni-plugin cni plugin ServiceAccount exists",
				"linkerd-cni-plugin cni plugin DaemonSet exists",
				"linkerd-cni-plugin cni plugin pod is running on all nodes",
			},
		},
	}

	for _, tc := range testCases {
		tc := tc // pin
		t.Run(tc.description, func(t *testing.T) {
			hc := NewHealthChecker(
				[]CategoryID{LinkerdCNIPluginChecks},
				&Options{
					CNINamespace: "test-ns",
				},
			)

			k8sConfigs := getFakeCniResources(tc.testCaseOpts)
			var err error
			hc.kubeAPI, err = k8s.NewFakeAPI(k8sConfigs...)
			hc.CNIEnabled = true
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}

			obs := newObserver()
			hc.RunChecks(obs.resultFn)
			if diff := deep.Equal(obs.results, tc.results); diff != nil {
				t.Fatalf("%+v", diff)
			}
		})
	}

}

func TestMinReplicaCheck(t *testing.T) {
	hc := NewHealthChecker(
		[]CategoryID{LinkerdHAChecks},
		&Options{
			ControlPlaneNamespace: "linkerd",
		},
	)

	var err error

	testCases := []struct {
		controlPlaneResourceDefs []string
		expected                 error
	}{
		{
			controlPlaneResourceDefs: generateAllControlPlaneDef(&controlPlaneReplicaOptions{
				destination:   1,
				identity:      3,
				proxyInjector: 3,
				tap:           3,
			}, t),
			expected: fmt.Errorf("not enough replicas available for [linkerd-destination]"),
		},
		{
			controlPlaneResourceDefs: generateAllControlPlaneDef(&controlPlaneReplicaOptions{
				destination:   2,
				identity:      1,
				proxyInjector: 1,
				tap:           3,
			}, t),
			expected: fmt.Errorf("not enough replicas available for [linkerd-identity linkerd-proxy-injector]"),
		},
		{
			controlPlaneResourceDefs: generateAllControlPlaneDef(&controlPlaneReplicaOptions{
				destination:   2,
				identity:      2,
				proxyInjector: 3,
				tap:           3,
			}, t),
			expected: nil,
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			hc.kubeAPI, err = k8s.NewFakeAPI(tc.controlPlaneResourceDefs...)
			if err != nil {
				t.Fatal(err)
			}
			err = hc.checkMinReplicasAvailable(context.Background())
			if err == nil && tc.expected != nil {
				t.Log("Expected error: nil")
				t.Logf("Received error: %s\n", err)
				t.Fatal("test case failed")
			}
			if err != nil {
				if err.Error() != tc.expected.Error() {
					t.Logf("Expected error: %s\n", tc.expected)
					t.Logf("Received error: %s\n", err)
					t.Fatal("test case failed")
				}
			}
		})
	}
}

func TestCheckOpaquePortAnnotations(t *testing.T) {
	hc := NewHealthChecker(
		[]CategoryID{LinkerdOpaquePortsDefinitionChecks},
		&Options{
			DataPlaneNamespace: "test-ns",
		},
	)

	var err error

	var testCases = []struct {
		resources []string
		expected  error
	}{
		{
			resources: []string{`
apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: test-ns
  annotations:
    config.linkerd.io/opaque-ports: "9200"
spec:
  selector:
    app: test
  ports:
  - name: test
    port: 9200
    targetPort: 9200
`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: pod
  namespace: test-ns
  labels:
    app: test
  annotations:
    config.linkerd.io/opaque-ports: "9200"
spec:
  containers:
  - name: test
    image: test
    ports:
    - name: test
      containerPort: 9200
`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: svc
  namespace: test-ns
subsets:
- addresses:
  - ip: 10.244.3.12
    nodeName: nod
    targetRef:
      kind: Pod
      name: pod
      namespace: test-ns
  ports:
  - name: test
    port: 9200
    protocol: TCP
`,
			},
		},
		{
			resources: []string{`
apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: test-ns
spec:
  selector:
    app: test
  ports:
  - name: http
    port: 9200
    targetPort: 9200
`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: pod
  namespace: test-ns
  labels:
    app: test
  annotations:
    config.linkerd.io/opaque-ports: "9200"
spec:
  containers:
  - name: test
    image: test
    ports:
    - name: test
      containerPort: 9200
`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: svc
  namespace: test-ns
subsets:
- addresses:
  - ip: 10.244.3.12
    nodeName: nod
    targetRef:
      kind: Pod
      name: pod
      namespace: test-ns
  ports:
  - name: test
    port: 9200
    protocol: TCP
`,
			},
			expected: fmt.Errorf("\t* service svc targets the opaque port 9200 through 9200; add 9200 to its config.linkerd.io/opaque-ports annotation"),
		},
		{
			resources: []string{`
apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: test-ns
  annotations:
    config.linkerd.io/opaque-ports: "9200"
spec:
  selector:
    app: test
  ports:
  - name: test
    port: 9200
    targetPort: 9200
`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: pod
  namespace: test-ns
  labels:
    app: test
spec:
  containers:
  - name: test
    image: test
    ports:
    - name: test
      containerPort: 9200
`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: svc
  namespace: test-ns
subsets:
- addresses:
  - ip: 10.244.3.12
    nodeName: nod
    targetRef:
      kind: Pod
      name: pod
      namespace: test-ns
  ports:
  - name: test
    port: 9200
    protocol: TCP
`,
			},
			expected: fmt.Errorf("\t* service svc expects target port 9200 to be opaque; add it to pod pod config.linkerd.io/opaque-ports annotation"),
		},
		{
			resources: []string{`
apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: test-ns
  annotations:
    config.linkerd.io/opaque-ports: "9200"
spec:
  selector:
    app: test
  ports:
    - name: test
      port: 9200
      targetPort: 9200
`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: pod
  namespace: test-ns
  labels:
    app: test
  annotations:
    config.linkerd.io/opaque-ports: "9300"
spec:
  containers:
  - name: test
    image: test
    ports:
    - name: test
      containerPort: 9300
`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: svc
  namespace: test-ns
subsets:
- addresses:
  - ip: 10.244.3.12
    nodeName: node
    targetRef:
      kind: Pod
      name: pod
      namespace: test-ns
  ports:
  - name: test
    port: 9200
    protocol: TCP
`,
			},
		},
		{
			resources: []string{`
apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: test-ns
spec:
  selector:
    app: test
  ports:
  - name: test
    port: 1002
    targetPort: 2002
`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: pod
  namespace: test-ns
  annotations:
    config.linkerd.io/opaque-ports: "2002"
  labels:
    app: test
spec:
  containers:
  - name: test
    image: test
    ports:
    - name: test
      containerPort: 2002
`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: svc
  namespace: test-ns
subsets:
- addresses:
  - ip: 10.42.0.111
    nodeName: node
    targetRef:
      kind: Pod
      name: pod
  ports:
  - name: test
    port: 2002
    protocol: TCP
`,
			},
			expected: fmt.Errorf("\t* service svc targets the opaque port 2002 through 1002; add 1002 to its config.linkerd.io/opaque-ports annotation"),
		},
		{
			resources: []string{`
apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: test-ns
spec:
  selector:
    app: test
  ports:
  - name: test
    port: 1003
    targetPort: pod-test
`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: pod
  namespace: test-ns
  annotations:
    config.linkerd.io/opaque-ports: "2003"
  labels:
    app: test
spec:
  containers:
  - name: test
    image: test
    ports:
    - name: pod-test
      containerPort: 2003
`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: svc
  namespace: test-ns
subsets:
- addresses:
  - ip: 10.42.0.112
    nodeName: node
    targetRef:
      kind: Pod
      name: pod
  ports:
  - name: test
    port: 2003
    protocol: TCP
`,
			},
			expected: fmt.Errorf("\t* service svc targets the opaque port pod-test through 1003; add 1003 to its config.linkerd.io/opaque-ports annotation"),
		},
		{
			resources: []string{`
apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: test-ns
spec:
  selector:
    app: test
  ports:
  - port: 80
    targetPort: 6502
`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: pod
  namespace: test-ns
  annotations:
    config.linkerd.io/opaque-ports: "5432"
  labels:
    app: test
spec:
  containers:
  - name: c1
    image: test
    ports:
    - containerPort: 6502
  - name: c2
    image: test
    ports:
    - containerPort: 5432
`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: svc
  namespace: test-ns
subsets:
- addresses:
  - ip: 10.42.0.112
    nodeName: node
    targetRef:
      kind: Pod
      name: pod
  ports:
  - port: 6502
    protocol: TCP
  - port: 5432
    protocol: TCP
`,
			},
			expected: nil,
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			hc.kubeAPI, err = k8s.NewFakeAPI(tc.resources...)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			err = hc.checkMisconfiguredOpaquePortAnnotations(context.Background())
			if err == nil && tc.expected != nil {
				t.Fatalf("Expected check to fail with %s", tc.expected.Error())
			}
			if err != nil && tc.expected != nil {
				if err.Error() != tc.expected.Error() {
					t.Fatalf("Expected error: %s, received: %s", tc.expected, err)
				}
			}
			if err != nil && tc.expected == nil {
				t.Fatalf("Did not expect error but got: %s", err.Error())
			}
		})
	}
}

type controlPlaneReplicaOptions struct {
	destination   int
	identity      int
	proxyInjector int
	tap           int
}

func getSingleControlPlaneDef(component string, availableReplicas int) string {
	return fmt.Sprintf(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: linkerd
spec:
  template:
    spec:
      containers:
        - image: "hello-world"
          name: test
status:
  availableReplicas: %d`, component, availableReplicas)
}

func generateAllControlPlaneDef(replicaOptions *controlPlaneReplicaOptions, t *testing.T) []string {
	resourceDefs := []string{}
	for _, component := range linkerdHAControlPlaneComponents {
		switch component {
		case "linkerd-destination":
			resourceDefs = append(resourceDefs, getSingleControlPlaneDef(component, replicaOptions.destination))
		case "linkerd-identity":
			resourceDefs = append(resourceDefs, getSingleControlPlaneDef(component, replicaOptions.identity))
		case "linkerd-proxy-injector":
			resourceDefs = append(resourceDefs, getSingleControlPlaneDef(component, replicaOptions.proxyInjector))
		case "linkerd-tap":
			resourceDefs = append(resourceDefs, getSingleControlPlaneDef(component, replicaOptions.tap))
		default:
			t.Fatal("Could not find the resource")
		}
	}
	return resourceDefs
}

func multiappend(slices ...[]string) []string {
	res := []string{}
	for _, slice := range slices {
		res = append(res, slice...)
	}
	return res
}
