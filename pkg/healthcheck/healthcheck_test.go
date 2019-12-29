package healthcheck

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/pkg/issuercerts"
	"github.com/linkerd/linkerd2/pkg/tls"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/linkerd/linkerd2/controller/api/public"
	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	configPb "github.com/linkerd/linkerd2/controller/gen/config"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/identity"
	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

func (hc *HealthChecker) addCheckAsCategory(
	testCategoryID CategoryID,
	categoryID CategoryID,
	desc string,
) {
	testCategory := category{
		id:       testCategoryID,
		checkers: []checker{},
	}

	for _, cat := range hc.categories {
		if cat.id == categoryID {
			for _, ch := range cat.checkers {
				if ch.description == desc {
					testCategory.checkers = append(testCategory.checkers, ch)
					break
				}
			}
			break
		}
	}
	hc.addCategory(testCategory)
}

func TestHealthChecker(t *testing.T) {
	nullObserver := func(*CheckResult) {}

	passingCheck1 := category{
		id: "cat1",
		checkers: []checker{
			{
				description: "desc1",
				check: func(context.Context) error {
					return nil
				},
				retryDeadline: time.Time{},
			},
		},
	}

	passingCheck2 := category{
		id: "cat2",
		checkers: []checker{
			{
				description: "desc2",
				check: func(context.Context) error {
					return nil
				},
				retryDeadline: time.Time{},
			},
		},
	}

	failingCheck := category{
		id: "cat3",
		checkers: []checker{
			{
				description: "desc3",
				check: func(context.Context) error {
					return fmt.Errorf("error")
				},
				retryDeadline: time.Time{},
			},
		},
	}

	passingRPCClient := public.MockAPIClient{
		SelfCheckResponseToReturn: &healthcheckPb.SelfCheckResponse{
			Results: []*healthcheckPb.CheckResult{
				{
					SubsystemName:    "rpc1",
					CheckDescription: "rpc desc1",
					Status:           healthcheckPb.CheckStatus_OK,
				},
			},
		},
	}

	passingRPCCheck := category{
		id: "cat4",
		checkers: []checker{
			{
				description: "desc4",
				checkRPC: func(context.Context) (*healthcheckPb.SelfCheckResponse, error) {
					return passingRPCClient.SelfCheck(context.Background(),
						&healthcheckPb.SelfCheckRequest{})
				},
				retryDeadline: time.Time{},
			},
		},
	}

	failingRPCClient := public.MockAPIClient{
		SelfCheckResponseToReturn: &healthcheckPb.SelfCheckResponse{
			Results: []*healthcheckPb.CheckResult{
				{
					SubsystemName:         "rpc2",
					CheckDescription:      "rpc desc2",
					Status:                healthcheckPb.CheckStatus_FAIL,
					FriendlyMessageToUser: "rpc error",
				},
			},
		},
	}

	failingRPCCheck := category{
		id: "cat5",
		checkers: []checker{
			{
				description: "desc5",
				checkRPC: func(context.Context) (*healthcheckPb.SelfCheckResponse, error) {
					return failingRPCClient.SelfCheck(context.Background(),
						&healthcheckPb.SelfCheckRequest{})
				},
				retryDeadline: time.Time{},
			},
		},
	}

	fatalCheck := category{
		id: "cat6",
		checkers: []checker{
			{
				description: "desc6",
				fatal:       true,
				check: func(context.Context) error {
					return fmt.Errorf("fatal")
				},
				retryDeadline: time.Time{},
			},
		},
	}

	skippingCheck := category{
		id: "cat7",
		checkers: []checker{
			{
				description: "skip",
				check: func(context.Context) error {
					return &SkipError{Reason: "needs skipping"}
				},
				retryDeadline: time.Time{},
			},
		},
	}

	skippingRPCCheck := category{
		id: "cat8",
		checkers: []checker{
			{
				description: "skipRpc",
				checkRPC: func(context.Context) (*healthcheckPb.SelfCheckResponse, error) {
					return nil, &SkipError{Reason: "needs skipping"}
				},
				retryDeadline: time.Time{},
			},
		},
	}

	t.Run("Notifies observer of all results", func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.addCategory(passingCheck1)
		hc.addCategory(passingCheck2)
		hc.addCategory(failingCheck)
		hc.addCategory(passingRPCCheck)
		hc.addCategory(failingRPCCheck)

		expectedResults := []string{
			"cat1 desc1",
			"cat2 desc2",
			"cat3 desc3: error",
			"cat4 desc4",
			"cat4 [rpc1] rpc desc1",
			"cat5 desc5",
			"cat5 [rpc2] rpc desc2: rpc error",
		}

		obs := newObserver()
		hc.RunChecks(obs.resultFn)

		if !reflect.DeepEqual(obs.results, expectedResults) {
			t.Fatalf("Expected results %v, but got %v", expectedResults, obs.results)
		}
	})

	t.Run("Is successful if all checks were successful", func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.addCategory(passingCheck1)
		hc.addCategory(passingCheck2)
		hc.addCategory(passingRPCCheck)

		success := hc.RunChecks(nullObserver)

		if !success {
			t.Fatalf("Expecting checks to be successful, but got [%t]", success)
		}
	})

	t.Run("Is not successful if one check fails", func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.addCategory(passingCheck1)
		hc.addCategory(failingCheck)
		hc.addCategory(passingCheck2)

		success := hc.RunChecks(nullObserver)

		if success {
			t.Fatalf("Expecting checks to not be successful, but got [%t]", success)
		}
	})

	t.Run("Is not successful if one RPC check fails", func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.addCategory(passingCheck1)
		hc.addCategory(failingRPCCheck)
		hc.addCategory(passingCheck2)

		success := hc.RunChecks(nullObserver)

		if success {
			t.Fatalf("Expecting checks to not be successful, but got [%t]", success)
		}
	})

	t.Run("Does not run remaining check if fatal check fails", func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.addCategory(passingCheck1)
		hc.addCategory(fatalCheck)
		hc.addCategory(passingCheck2)

		expectedResults := []string{
			"cat1 desc1",
			"cat6 desc6: fatal",
		}

		obs := newObserver()
		hc.RunChecks(obs.resultFn)

		if !reflect.DeepEqual(obs.results, expectedResults) {
			t.Fatalf("Expected results %v, but got %v", expectedResults, obs.results)
		}
	})

	t.Run("Retries checks if retry is specified", func(t *testing.T) {
		retryWindow = 0
		returnError := true

		retryCheck := category{
			id: "cat7",
			checkers: []checker{
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
		}

		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.addCategory(passingCheck1)
		hc.addCategory(retryCheck)

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

		if !reflect.DeepEqual(observedResults, expectedResults) {
			t.Fatalf("Expected results %v, but got %v", expectedResults, observedResults)
		}
	})

	t.Run("Does not notify observer of skipped checks", func(t *testing.T) {
		hc := NewHealthChecker(
			[]CategoryID{},
			&Options{},
		)
		hc.addCategory(passingCheck1)
		hc.addCategory(skippingCheck)
		hc.addCategory(skippingRPCCheck)

		expectedResults := []string{
			"cat1 desc1",
		}

		obs := newObserver()
		hc.RunChecks(obs.resultFn)

		if !reflect.DeepEqual(obs.results, expectedResults) {
			t.Fatalf("Expected results %v, but got %v", expectedResults, obs.results)
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
	err = hc.checkCanCreate("", "apps", "v1", "deployments")
	if err == nil ||
		err.Error() != exp.Error() {
		t.Fatalf("Unexpected error (Expected: %s, Got: %s)", exp, err)
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

			err = hc.checkClockSkew()
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

func TestChecCapability(t *testing.T) {
	tests := []struct {
		k8sConfigs []string
		err        error
	}{
		{
			[]string{},
			nil,
		},
		{
			[]string{`apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: restricted
spec:
  requiredDropCapabilities:
    - ALL`,
			},
			fmt.Errorf("found 1 PodSecurityPolicies, but none provide TEST_CAP, proxy injection will fail if the PSP admission controller is running"),
		},
	}

	for i, test := range tests {
		test := test // pin
		t.Run(fmt.Sprintf("%d: returns expected capability result", i), func(t *testing.T) {
			hc := NewHealthChecker(
				[]CategoryID{},
				&Options{},
			)

			var err error
			hc.kubeAPI, err = k8s.NewFakeAPI(test.k8sConfigs...)
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}

			err = hc.checkCapability("TEST_CAP")
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
	testCases := []struct {
		k8sConfigs []string
		results    []string
	}{
		{
			[]string{},
			[]string{"linkerd-config control plane Namespace exists: The \"test-ns\" namespace does not exist"},
		},
		{
			[]string{`
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
`,
			},
			[]string{
				"linkerd-config control plane Namespace exists",
				"linkerd-config control plane ClusterRoles exist: missing ClusterRoles: linkerd-test-ns-controller, linkerd-test-ns-identity, linkerd-test-ns-prometheus, linkerd-test-ns-proxy-injector, linkerd-test-ns-sp-validator, linkerd-test-ns-tap",
			},
		},
		{
			[]string{`
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-controller
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
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
  name: linkerd-test-ns-prometheus
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
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-sp-validator
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-tap
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
			},
			[]string{
				"linkerd-config control plane Namespace exists",
				"linkerd-config control plane ClusterRoles exist",
				"linkerd-config control plane ClusterRoleBindings exist: missing ClusterRoleBindings: linkerd-test-ns-controller, linkerd-test-ns-identity, linkerd-test-ns-prometheus, linkerd-test-ns-proxy-injector, linkerd-test-ns-sp-validator, linkerd-test-ns-tap",
			},
		},
		{
			[]string{`
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-controller
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
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
  name: linkerd-test-ns-prometheus
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
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-sp-validator
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-tap
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-controller
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
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
  name: linkerd-test-ns-prometheus
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
`,
				`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-sp-validator
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-tap
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-controller
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
  name: linkerd-prometheus
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
  name: linkerd-sp-validator
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-grafana
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
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-web
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-tap
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
			},
			[]string{
				"linkerd-config control plane Namespace exists",
				"linkerd-config control plane ClusterRoles exist",
				"linkerd-config control plane ClusterRoleBindings exist",
				"linkerd-config control plane ServiceAccounts exist",
				"linkerd-config control plane CustomResourceDefinitions exist: missing CustomResourceDefinitions: serviceprofiles.linkerd.io",
			},
		},
		{
			[]string{`
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-controller
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
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
  name: linkerd-test-ns-prometheus
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
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-sp-validator
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-tap
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-controller
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
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
  name: linkerd-test-ns-prometheus
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
`,
				`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-sp-validator
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-tap
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-controller
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
  name: linkerd-prometheus
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
  name: linkerd-sp-validator
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-grafana
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
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-web
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-tap
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: serviceprofiles.linkerd.io
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
			},
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
			[]string{`
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-controller
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
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
  name: linkerd-test-ns-prometheus
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
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-sp-validator
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-tap
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-controller
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
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
  name: linkerd-test-ns-prometheus
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
`,
				`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-sp-validator
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-tap
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-controller
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
  name: linkerd-prometheus
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
  name: linkerd-sp-validator
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-grafana
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
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-web
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-tap
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: serviceprofiles.linkerd.io
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
apiVersion: admissionregistration.k8s.io/v1beta1
kind: MutatingWebhookConfiguration
metadata:
  name: linkerd-proxy-injector-webhook-config
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
			},
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
			[]string{`
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-controller
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
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
  name: linkerd-test-ns-prometheus
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
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-sp-validator
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-tap
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-controller
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
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
  name: linkerd-test-ns-prometheus
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
`,
				`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-sp-validator
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-tap
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-controller
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
  name: linkerd-prometheus
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
  name: linkerd-sp-validator
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-grafana
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
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-web
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-tap
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: serviceprofiles.linkerd.io
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
apiVersion: admissionregistration.k8s.io/v1beta1
kind: MutatingWebhookConfiguration
metadata:
  name: linkerd-proxy-injector-webhook-config
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
apiVersion: admissionregistration.k8s.io/v1beta1
kind: ValidatingWebhookConfiguration
metadata:
  name: linkerd-sp-validator-webhook-config
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
			},
			[]string{
				"linkerd-config control plane Namespace exists",
				"linkerd-config control plane ClusterRoles exist",
				"linkerd-config control plane ClusterRoleBindings exist",
				"linkerd-config control plane ServiceAccounts exist",
				"linkerd-config control plane CustomResourceDefinitions exist",
				"linkerd-config control plane MutatingWebhookConfigurations exist",
				"linkerd-config control plane ValidatingWebhookConfigurations exist",
				"linkerd-config control plane PodSecurityPolicies exist: missing PodSecurityPolicies: linkerd-test-ns-control-plane",
			},
		},
		{
			[]string{`
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-controller
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
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
  name: linkerd-test-ns-prometheus
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
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-sp-validator
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-tap
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-controller
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
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
  name: linkerd-test-ns-prometheus
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
`,
				`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-sp-validator
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: linkerd-test-ns-tap
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-controller
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
  name: linkerd-prometheus
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
  name: linkerd-sp-validator
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-grafana
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
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-web
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
kind: ServiceAccount
apiVersion: v1
metadata:
  name: linkerd-tap
  namespace: test-ns
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: serviceprofiles.linkerd.io
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
apiVersion: admissionregistration.k8s.io/v1beta1
kind: MutatingWebhookConfiguration
metadata:
  name: linkerd-proxy-injector-webhook-config
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
apiVersion: admissionregistration.k8s.io/v1beta1
kind: ValidatingWebhookConfiguration
metadata:
  name: linkerd-sp-validator-webhook-config
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
				`
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: linkerd-test-ns-control-plane
  labels:
    linkerd.io/control-plane-ns: test-ns
`,
			},
			[]string{
				"linkerd-config control plane Namespace exists",
				"linkerd-config control plane ClusterRoles exist",
				"linkerd-config control plane ClusterRoleBindings exist",
				"linkerd-config control plane ServiceAccounts exist",
				"linkerd-config control plane CustomResourceDefinitions exist",
				"linkerd-config control plane MutatingWebhookConfigurations exist",
				"linkerd-config control plane ValidatingWebhookConfigurations exist",
				"linkerd-config control plane PodSecurityPolicies exist",
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
				},
			)

			var err error
			hc.kubeAPI, err = k8s.NewFakeAPI(tc.k8sConfigs...)
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}

			obs := newObserver()
			hc.RunChecks(obs.resultFn)
			if !reflect.DeepEqual(obs.results, tc.results) {
				t.Fatalf("Expected results\n%s,\nbut got:\n%s", strings.Join(tc.results, "\n"), strings.Join(obs.results, "\n"))
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
			checkDescription: "controller pod is running",
			resources: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: linkerd-controller-6f78cbd47-bc557
  namespace: test-ns
status:
  phase: Running
  podIP: 1.2.3.4
`,
			},
			expected: []string{
				"cat1 controller pod is running",
			},
		},
		{
			checkDescription: "'linkerd-config' config map exists",
			resources: []string{`
apiVersion: v1
kind: ConfigMap
metadata:
  name: linkerd-config
  namespace: test-ns
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
			if !reflect.DeepEqual(obs.results, testCase.expected) {
				t.Fatalf("Expected results %v, but got %v", testCase.expected, obs.results)
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

	linkerdConfigMap := fmt.Sprintf(`
kind: ConfigMap
apiVersion: v1
metadata:
  name: %s
data:
  global: |
    {"identityContext":{"trustAnchorsPem": "%s"}}
`, k8s.ConfigConfigMapName, currentCertificate)

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
			hc.kubeAPI, err = k8s.NewFakeAPI(append(testCase.resources, linkerdConfigMap)...)
			if err != nil {
				t.Fatalf("Unexpected error: %q", err)
			}

			err = hc.checkDataPlaneProxiesCertificate()
			if !reflect.DeepEqual(err, testCase.expectedErr) {
				t.Fatalf("Error %q does not match expected error: %q", err, testCase.expectedErr)
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
			pod("linkerd-controller-6f78cbd47-bc557", corev1.PodRunning, true),
			pod("linkerd-grafana-5b7d796646-hh46d", corev1.PodRunning, true),
			pod("linkerd-identity-6849948664-27982", corev1.PodRunning, true),
			pod("linkerd-prometheus-74d6879cd6-bbdk6", corev1.PodFailed, false),
			pod("linkerd-tap-6c878df6c8-2hmtd", corev1.PodRunning, true),
			pod("linkerd-sp-validator-24d2879ce6-cddk9", corev1.PodRunning, true),
			pod("linkerd-web-98c9ddbcd-7b5lh", corev1.PodRunning, true),
		}

		err := validateControlPlanePods(pods)
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "No running pods for \"linkerd-prometheus\"" {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns an error if not all containers are ready", func(t *testing.T) {
		pods := []corev1.Pod{
			pod("linkerd-controller-6f78cbd47-bc557", corev1.PodRunning, true),
			pod("linkerd-grafana-5b7d796646-hh46d", corev1.PodRunning, false),
			pod("linkerd-identity-6849948664-27982", corev1.PodRunning, true),
			pod("linkerd-prometheus-74d6879cd6-bbdk6", corev1.PodRunning, true),
			pod("linkerd-tap-6c878df6c8-2hmtd", corev1.PodRunning, true),
			pod("linkerd-sp-validator-24d2879ce6-cddk9", corev1.PodRunning, true),
			pod("linkerd-web-98c9ddbcd-7b5lh", corev1.PodRunning, true),
		}

		err := validateControlPlanePods(pods)
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "pod/linkerd-grafana-5b7d796646-hh46d container grafana is not ready" {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns nil if all pods are running and all containers are ready", func(t *testing.T) {
		pods := []corev1.Pod{
			pod("linkerd-controller-6f78cbd47-bc557", corev1.PodRunning, true),
			pod("linkerd-grafana-5b7d796646-hh46d", corev1.PodRunning, true),
			pod("linkerd-identity-6849948664-27982", corev1.PodRunning, true),
			pod("linkerd-prometheus-74d6879cd6-bbdk6", corev1.PodRunning, true),
			pod("linkerd-sp-validator-24d2879ce6-cddk9", corev1.PodRunning, true),
			pod("linkerd-tap-6c878df6c8-2hmtd", corev1.PodRunning, true),
			pod("linkerd-web-98c9ddbcd-7b5lh", corev1.PodRunning, true),
		}

		err := validateControlPlanePods(pods)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})

	t.Run("Returns nil if, HA mode, at least one pod of each control plane component is ready", func(t *testing.T) {
		pods := []corev1.Pod{
			pod("linkerd-controller-6f78cbd47-bc557", corev1.PodRunning, true),
			pod("linkerd-controller-6f78cbd47-bc558", corev1.PodRunning, false),
			pod("linkerd-controller-6f78cbd47-bc559", corev1.PodFailed, false),
			pod("linkerd-grafana-5b7d796646-hh46d", corev1.PodRunning, true),
			pod("linkerd-identity-6849948664-27982", corev1.PodRunning, true),
			pod("linkerd-identity-6849948664-27983", corev1.PodRunning, false),
			pod("linkerd-identity-6849948664-27984", corev1.PodFailed, false),
			pod("linkerd-tap-6c878df6c8-2hmtd", corev1.PodRunning, true),
			pod("linkerd-prometheus-74d6879cd6-bbdk6", corev1.PodRunning, true),
			pod("linkerd-sp-validator-24d2879ce6-cddk9", corev1.PodRunning, true),
			pod("linkerd-web-98c9ddbcd-7b5lh", corev1.PodRunning, true),
		}

		err := validateControlPlanePods(pods)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})

	t.Run("Returns nil if all linkerd pods are running and pod list includes non-linkerd pod", func(t *testing.T) {
		pods := []corev1.Pod{
			pod("linkerd-controller-6f78cbd47-bc557", corev1.PodRunning, true),
			pod("linkerd-grafana-5b7d796646-hh46d", corev1.PodRunning, true),
			pod("linkerd-identity-6849948664-27982", corev1.PodRunning, true),
			pod("linkerd-prometheus-74d6879cd6-bbdk6", corev1.PodRunning, true),
			pod("linkerd-sp-validator-24d2879ce6-cddk9", corev1.PodRunning, true),
			pod("linkerd-tap-6c878df6c8-2hmtd", corev1.PodRunning, true),
			pod("linkerd-web-98c9ddbcd-7b5lh", corev1.PodRunning, true),
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

			// create a synethic category that only includes the "data plane namespace exists" check
			hc.addCheckAsCategory("data-plane-ns-test-cat", LinkerdDataPlaneChecks, "data plane namespace exists")

			expectedResults := []string{
				tc.result,
			}
			obs := newObserver()
			hc.RunChecks(obs.resultFn)
			if !reflect.DeepEqual(obs.results, expectedResults) {
				t.Fatalf("Expected results %v, but got %v", expectedResults, obs.results)
			}
		})
	}
}

func TestValidateDataPlanePods(t *testing.T) {

	t.Run("Returns an error if no inject pods were found", func(t *testing.T) {
		err := validateDataPlanePods([]*pb.Pod{}, "emojivoto")
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "No \"linkerd-proxy\" containers found in the \"emojivoto\" namespace" {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns an error if not all pods are running", func(t *testing.T) {
		pods := []*pb.Pod{
			{Name: "emoji-d9c7866bb-7v74n", Status: "Running", ProxyReady: true},
			{Name: "vote-bot-644b8cb6b4-g8nlr", Status: "Running", ProxyReady: true},
			{Name: "voting-65b9fffd77-rlwsd", Status: "Failed", ProxyReady: false},
			{Name: "web-6cfbccc48-5g8px", Status: "Running", ProxyReady: true},
		}

		err := validateDataPlanePods(pods, "emojivoto")
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "The \"voting-65b9fffd77-rlwsd\" pod is not running" {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns an error if the proxy container is not ready", func(t *testing.T) {
		pods := []*pb.Pod{
			{Name: "emoji-d9c7866bb-7v74n", Status: "Running", ProxyReady: true},
			{Name: "vote-bot-644b8cb6b4-g8nlr", Status: "Running", ProxyReady: false},
			{Name: "voting-65b9fffd77-rlwsd", Status: "Running", ProxyReady: true},
			{Name: "web-6cfbccc48-5g8px", Status: "Running", ProxyReady: true},
		}

		err := validateDataPlanePods(pods, "emojivoto")
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "The \"linkerd-proxy\" container in the \"vote-bot-644b8cb6b4-g8nlr\" pod is not ready" {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns nil if all pods are running and all proxy containers are ready", func(t *testing.T) {
		pods := []*pb.Pod{
			{Name: "emoji-d9c7866bb-7v74n", Status: "Running", ProxyReady: true},
			{Name: "vote-bot-644b8cb6b4-g8nlr", Status: "Running", ProxyReady: true},
			{Name: "voting-65b9fffd77-rlwsd", Status: "Running", ProxyReady: true},
			{Name: "web-6cfbccc48-5g8px", Status: "Running", ProxyReady: true},
		}

		err := validateDataPlanePods(pods, "emojivoto")
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})
}

func TestValidateDataPlanePodReporting(t *testing.T) {
	t.Run("Returns success if no pods present", func(t *testing.T) {
		err := validateDataPlanePodReporting([]*pb.Pod{})
		if err != nil {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns success if all pods are added", func(t *testing.T) {
		pods := []*pb.Pod{
			{Name: "ns1/test1", Added: true},
			{Name: "ns2/test2", Added: true},
		}

		err := validateDataPlanePodReporting(pods)
		if err != nil {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("Returns an error if any of the pod was not added to Prometheus", func(t *testing.T) {
		pods := []*pb.Pod{
			{Name: "ns1/test1", Added: true},
			{Name: "ns2/test2", Added: false},
		}

		err := validateDataPlanePodReporting(pods)
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != "Data plane metrics not found for ns2/test2." {
			t.Fatalf("Unexpected error message: %s", err.Error())
		}
	})
}

func TestLinkerdPreInstallGlobalResourcesChecks(t *testing.T) {
	hc := NewHealthChecker(
		[]CategoryID{LinkerdPreInstallGlobalResourcesChecks},
		&Options{})

	t.Run("global resources don't exist", func(t *testing.T) {
		var err error
		hc.kubeAPI, err = k8s.NewFakeAPI()
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		observer := newObserver()
		if !hc.RunChecks(observer.resultFn) {
			t.Errorf("Expect RunChecks to return true")
		}

		expected := []string{
			"pre-linkerd-global-resources no ClusterRoles exist",
			"pre-linkerd-global-resources no ClusterRoleBindings exist",
			"pre-linkerd-global-resources no CustomResourceDefinitions exist",
			"pre-linkerd-global-resources no MutatingWebhookConfigurations exist",
			"pre-linkerd-global-resources no ValidatingWebhookConfigurations exist",
			"pre-linkerd-global-resources no PodSecurityPolicies exist",
		}
		if !reflect.DeepEqual(observer.results, expected) {
			t.Errorf("Mismatch result.\nExpected: %v\n Actual: %v\n", expected, observer.results)
		}
	})

	t.Run("global resources exist", func(t *testing.T) {
		resources := []string{
			`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cluster-role
  labels:
    linkerd.io/control-plane-ns: test-ns`,
			`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: cluster-role-binding
  labels:
    linkerd.io/control-plane-ns: test-ns`,
			`apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: custom-resource-definition
  labels:
    linkerd.io/control-plane-ns: test-ns`,
			`apiVersion: admissionregistration.k8s.io/v1beta1
kind: MutatingWebhookConfiguration
metadata:
  name: mutating-webhook-configuration
  labels:
    linkerd.io/control-plane-ns: test-ns`,
			`apiVersion: admissionregistration.k8s.io/v1beta1
kind: ValidatingWebhookConfiguration
metadata:
  name: validating-webhook-configuration
  labels:
    linkerd.io/control-plane-ns: test-ns`,
			`apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: pod-security-policy
  labels:
    linkerd.io/control-plane-ns: test-ns`,
		}

		var err error
		hc.kubeAPI, err = k8s.NewFakeAPI(resources...)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		observer := newObserver()
		if hc.RunChecks(observer.resultFn) {
			t.Errorf("Expect RunChecks to return false")
		}

		expected := []string{
			"pre-linkerd-global-resources no ClusterRoles exist: ClusterRoles found but should not exist: cluster-role",
			"pre-linkerd-global-resources no ClusterRoleBindings exist: ClusterRoleBindings found but should not exist: cluster-role-binding",
			"pre-linkerd-global-resources no CustomResourceDefinitions exist: CustomResourceDefinitions found but should not exist: custom-resource-definition",
			"pre-linkerd-global-resources no MutatingWebhookConfigurations exist: MutatingWebhookConfigurations found but should not exist: mutating-webhook-configuration",
			"pre-linkerd-global-resources no ValidatingWebhookConfigurations exist: ValidatingWebhookConfigurations found but should not exist: validating-webhook-configuration",
			"pre-linkerd-global-resources no PodSecurityPolicies exist: PodSecurityPolicies found but should not exist: pod-security-policy",
		}
		if !reflect.DeepEqual(observer.results, expected) {
			t.Errorf("Mismatch result.\nExpected: %v\n Actual: %v\n", expected, observer.results)
		}
	})
}

func getConfigAndKubeSystemNamespace(ha bool, nsLabel string) []string {
	return []string{fmt.Sprintf(`
kind: ConfigMap
apiVersion: v1
metadata:
  name: linkerd-config
  namespace: linkerd
data:
  install: |
    {"cliVersion":"dev-undefined","flags":[{"name":"ha","value":"%v"}]}`, ha),
		fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  creationTimestamp: null
  labels:
    %s
  name: kube-system`, nsLabel),
	}
}

func TestKubeSystemNamespaceInHA(t *testing.T) {
	testCases := []struct {
		testDescription string
		k8sConfigs      []string
		expectedOutput  string
	}{
		{
			"passes when HA is not enabled",
			getConfigAndKubeSystemNamespace(false, ""),
			"",
		},
		{
			"passes when HA is enabled and namespace has required metadata",
			getConfigAndKubeSystemNamespace(true, "config.linkerd.io/admission-webhooks: disabled"),
			"l5d-injection-disabled pod injection disabled on kube-system",
		},
		{
			"fails when HA and admission hooks are enabled",
			getConfigAndKubeSystemNamespace(true, "config.linkerd.io/admission-webhooks: enabled"),
			"l5d-injection-disabled pod injection disabled on kube-system: kube-system namespace needs to have the label config.linkerd.io/admission-webhooks: disabled if HA mode is enabled",
		},
		{
			"fails when HA is enabled and metadata is missing",
			getConfigAndKubeSystemNamespace(true, ""),
			"l5d-injection-disabled pod injection disabled on kube-system: kube-system namespace needs to have the label config.linkerd.io/admission-webhooks: disabled if HA mode is enabled",
		},
	}

	for _, tc := range testCases {
		tc := tc // pin
		t.Run(tc.testDescription, func(t *testing.T) {

			hc := NewHealthChecker([]CategoryID{}, &Options{})
			hc.ControlPlaneNamespace = "linkerd"

			var err error
			hc.kubeAPI, _ = k8s.NewFakeAPI(tc.k8sConfigs...)
			_, hc.linkerdConfig, err = hc.checkLinkerdConfigConfigMap()
			if err != nil {
				t.Fatalf("Unexpected error: %q", err)
			}

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

				if !reflect.DeepEqual(obs.results, expectedResults) {
					t.Fatalf("Expected results %v, but got %v", expectedResults, obs.results)
				}
			}
		})
	}

}

func TestFetchLinkerdConfigMap(t *testing.T) {
	testCases := []struct {
		k8sConfigs []string
		expected   *configPb.All
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
    {"proxyImage":{"imageName":"gcr.io/linkerd-io/proxy","pullPolicy":"IfNotPresent"},"proxyInitImage":{"imageName":"gcr.io/linkerd-io/proxy-init","pullPolicy":"IfNotPresent"},"controlPort":{"port":4190},"ignoreInboundPorts":[],"ignoreOutboundPorts":[],"inboundPort":{"port":4143},"adminPort":{"port":4191},"outboundPort":{"port":4140},"resource":{"requestCpu":"","requestMemory":"","limitCpu":"","limitMemory":""},"proxyUid":"2102","logLevel":{"level":"warn,linkerd=info"},"disableExternalProfiles":true,"proxyVersion":"install-proxy-version", "proxy_init_image_version":"v1.3.0"}
  install: |
    {"cliVersion":"dev-undefined","flags":[]}
  debug: |
    {"debugImage":{"imageName":"gcr.io/linkerd-io/debug","pullPolicy":"IfNotPresent"},"debugImageVersion":"install-debug-version"}`,
			},
			&configPb.All{
				Global: &configPb.Global{
					LinkerdNamespace: "linkerd",
					Version:          "install-control-plane-version",
					IdentityContext: &configPb.IdentityContext{
						TrustDomain:     "cluster.local",
						TrustAnchorsPem: "fake-trust-anchors-pem",
						IssuanceLifetime: &duration.Duration{
							Seconds: 86400,
						},
						ClockSkewAllowance: &duration.Duration{
							Seconds: 20,
						},
					},
				}, Proxy: &configPb.Proxy{
					ProxyImage: &configPb.Image{
						ImageName:  "gcr.io/linkerd-io/proxy",
						PullPolicy: "IfNotPresent",
					},
					ProxyInitImage: &configPb.Image{
						ImageName:  "gcr.io/linkerd-io/proxy-init",
						PullPolicy: "IfNotPresent",
					},
					ControlPort: &configPb.Port{
						Port: 4190,
					},
					InboundPort: &configPb.Port{
						Port: 4143,
					},
					AdminPort: &configPb.Port{
						Port: 4191,
					},
					OutboundPort: &configPb.Port{
						Port: 4140,
					},
					Resource: &configPb.ResourceRequirements{},
					ProxyUid: 2102,
					LogLevel: &configPb.LogLevel{
						Level: "warn,linkerd=info",
					},
					DisableExternalProfiles: true,
					ProxyVersion:            "install-proxy-version",
					ProxyInitImageVersion:   "v1.3.0",
				}, Install: &configPb.Install{
					CliVersion: "dev-undefined",
				}, Debug: &configPb.Debug{
					DebugImage: &configPb.Image{
						ImageName:  "gcr.io/linkerd-io/debug",
						PullPolicy: "IfNotPresent",
					},
					DebugImageVersion: "install-debug-version",
				}},
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
    {"linkerdNamespace":"ns","identityContext":null}
  proxy: "{}"
  install: "{}"
  debug: "{}"`,
			},
			&configPb.All{Global: &configPb.Global{LinkerdNamespace: "ns", IdentityContext: nil}, Proxy: &configPb.Proxy{}, Install: &configPb.Install{}, Debug: &configPb.Debug{}},
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
  global: "{}"
  proxy: "{}"
  install: "{}"
  debug: "{}"`,
			},
			&configPb.All{Global: &configPb.Global{}, Proxy: &configPb.Proxy{}, Install: &configPb.Install{}, Debug: &configPb.Debug{}},
			nil,
		},
		{
			nil,
			nil,
			k8sErrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "linkerd-config"),
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			clientset, err := k8s.NewFakeAPI(tc.k8sConfigs...)
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}

			_, configs, err := FetchLinkerdConfigMap(clientset, "linkerd")
			if !reflect.DeepEqual(err, tc.err) {
				t.Fatalf("Expected \"%+v\", got \"%+v\"", tc.err, err)
			}

			if !proto.Equal(configs, tc.expected) {
				t.Fatalf("Unexpected config:\nExpected:\n%+v\nGot:\n%+v", tc.expected, configs)
			}
		})
	}
}

func getFakeConfig(secretScheme string, schemeInConfig string, issuerCerts *issuercerts.IssuerCertData) []string {

	var resources []string
	base64.StdEncoding.EncodeToString([]byte(issuerCerts.IssuerCrt))

	if secretScheme == k8s.IdentityIssuerSchemeLinkerd {
		resources = append(resources, fmt.Sprintf(`
kind: Secret
apiVersion: v1
metadata:
  name: linkerd-identity-issuer
  namespace: linkerd
data:
  crt.pem: %s
  key.pem: %s
---
`, base64.StdEncoding.EncodeToString([]byte(issuerCerts.IssuerCrt)), base64.StdEncoding.EncodeToString([]byte(issuerCerts.IssuerKey))))
	} else {
		resources = append(resources, fmt.Sprintf(
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
`, base64.StdEncoding.EncodeToString([]byte(issuerCerts.TrustAnchors)), base64.StdEncoding.EncodeToString([]byte(issuerCerts.IssuerCrt)), base64.StdEncoding.EncodeToString([]byte(issuerCerts.IssuerKey))))
	}

	anchors, _ := json.Marshal(issuerCerts.TrustAnchors)

	resources = append(resources, fmt.Sprintf(`
kind: ConfigMap
apiVersion: v1
metadata:
  name: linkerd-config
  namespace: linkerd
data:
  global: |
    {"linkerdNamespace": "linkerd", "identityContext":{"trustAnchorsPem": %s, "trustDomain": "cluster.local", "scheme": "%s"}}
---
`, anchors, schemeInConfig))

	return resources
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

func TestLinkerdIdentityCheck(t *testing.T) {
	type lifeSpan struct {
		starts time.Time
		ends   time.Time
	}
	var testCases = []struct {
		checkDescription   string
		certificateDNSName string
		lifespan           *lifeSpan
		tlsSecretScheme    string
		schemeInConfig     string
		expectedOutput     []string
		checkerToTest      string
	}{
		{
			checkerToTest:    "certificate config is valid",
			checkDescription: "works with valid cert and linkerd.io/tls secret",
			tlsSecretScheme:  k8s.IdentityIssuerSchemeLinkerd,
			schemeInConfig:   k8s.IdentityIssuerSchemeLinkerd,
			expectedOutput:   []string{"linkerd-identity-test-cat certificate config is valid"},
		},

		{
			checkerToTest:    "certificate config is valid",
			checkDescription: "works with valid cert and kubernetes.io/tls secret",
			tlsSecretScheme:  string(corev1.SecretTypeTLS),
			schemeInConfig:   string(corev1.SecretTypeTLS),
			expectedOutput:   []string{"linkerd-identity-test-cat certificate config is valid"},
		},
		{
			checkerToTest:    "certificate config is valid",
			checkDescription: "works if config scheme is empty and secret scheme is linkerd.io/tls (pre 2.7)",
			tlsSecretScheme:  k8s.IdentityIssuerSchemeLinkerd,
			schemeInConfig:   "",
			expectedOutput:   []string{"linkerd-identity-test-cat certificate config is valid"},
		},
		{
			checkerToTest:    "certificate config is valid",
			checkDescription: "fails if config scheme is empty and secret scheme is kubernetes.io/tls (pre 2.7)",
			tlsSecretScheme:  string(corev1.SecretTypeTLS),
			schemeInConfig:   "",
			expectedOutput:   []string{"linkerd-identity-test-cat certificate config is valid: key crt.pem containing the issuer certificate needs to exist in secret linkerd-identity-issuer if --identity-external-issuer=false"},
		},
		{
			checkerToTest:    "certificate config is valid",
			checkDescription: "fails when config scheme is linkerd.io/tls but secret scheme is kubernetes.io/tls in config is different than the one in the issuer secret",
			tlsSecretScheme:  string(corev1.SecretTypeTLS),
			schemeInConfig:   k8s.IdentityIssuerSchemeLinkerd,
			expectedOutput:   []string{"linkerd-identity-test-cat certificate config is valid: key crt.pem containing the issuer certificate needs to exist in secret linkerd-identity-issuer if --identity-external-issuer=false"},
		},
		{
			checkerToTest:    "certificate config is valid",
			checkDescription: "fails when config scheme is kubernetes.io/tls but secret scheme is linkerd.io/tls in config is different than the one in the issuer secret",
			tlsSecretScheme:  k8s.IdentityIssuerSchemeLinkerd,
			schemeInConfig:   string(corev1.SecretTypeTLS),
			expectedOutput:   []string{"linkerd-identity-test-cat certificate config is valid: key ca.crt containing the trust anchors needs to exist in secret linkerd-identity-issuer if --identity-external-issuer=true"},
		},
		{
			checkerToTest:      "issuer cert is issued by the trust root",
			checkDescription:   "fails when cert dns is wrong",
			certificateDNSName: "wrong.linkerd.cluster.local",
			expectedOutput:     []string{"linkerd-identity-test-cat issuer cert is issued by the trust root: x509: certificate is valid for wrong.linkerd.cluster.local, not identity.linkerd.cluster.local"},
		},
		{
			checkerToTest:    "trust roots are within their validity period",
			checkDescription: "fails when the only root cert is not valid yet",
			lifespan: &lifeSpan{
				starts: time.Date(2100, 1, 1, 1, 1, 1, 1, time.UTC),
				ends:   time.Date(2101, 1, 1, 1, 1, 1, 1, time.UTC),
			},
			expectedOutput: []string{"linkerd-identity-test-cat trust roots are within their validity period: Invalid roots:\n\t* 1 identity.linkerd.cluster.local not valid before: 2100-01-01T01:00:51Z"},
		},
		{
			checkerToTest:    "trust roots are within their validity period",
			checkDescription: "fails when the only root cert is expired",
			lifespan: &lifeSpan{
				starts: time.Date(1989, 1, 1, 1, 1, 1, 1, time.UTC),
				ends:   time.Date(1990, 1, 1, 1, 1, 1, 1, time.UTC),
			},
			expectedOutput: []string{"linkerd-identity-test-cat trust roots are within their validity period: Invalid roots:\n\t* 1 identity.linkerd.cluster.local not valid anymore. Expired on 1990-01-01T01:01:11Z"},
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

		if testCase.certificateDNSName == "" {
			testCase.certificateDNSName = "identity.linkerd.cluster.local"
		}
		if testCase.lifespan == nil {
			testCase.lifespan = &lifeSpan{
				starts: time.Now().AddDate(-1, 0, 0),
				ends:   time.Now().AddDate(1, 0, 0),
			}
		}

		if testCase.schemeInConfig == "" && testCase.tlsSecretScheme == "" {
			testCase.schemeInConfig = k8s.IdentityIssuerSchemeLinkerd
			testCase.tlsSecretScheme = k8s.IdentityIssuerSchemeLinkerd
		}

		t.Run(fmt.Sprintf("%d/%s", id, testCase.checkDescription), func(t *testing.T) {
			hc := NewHealthChecker(
				[]CategoryID{},
				&Options{
					DataPlaneNamespace: "linkerd",
				},
			)
			hc.addCheckAsCategory("linkerd-identity-test-cat", LinkerdIdentity, testCase.checkerToTest)
			var err error
			hc.ControlPlaneNamespace = "linkerd"
			issuerData := createIssuerData(testCase.certificateDNSName, testCase.lifespan.starts, testCase.lifespan.ends)
			config := getFakeConfig(testCase.tlsSecretScheme, testCase.schemeInConfig, issuerData)
			hc.kubeAPI, err = k8s.NewFakeAPI(config...)
			_, hc.linkerdConfig, _ = hc.checkLinkerdConfigConfigMap()

			if testCase.checkDescription != "certificate config is valid" {
				hc.issuerCert, hc.roots, _ = hc.checkCertificatesConfig()
			}

			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}

			obs := newObserver()
			hc.RunChecks(obs.resultFn)
			if !reflect.DeepEqual(obs.results, testCase.expectedOutput) {
				t.Fatalf("Expected results %v, but got %v", testCase.expectedOutput, obs.results)
			}
		})
	}
}
