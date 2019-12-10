package healthcheck

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"k8s.io/client-go/tools/record"

	"github.com/linkerd/linkerd2/pkg/issuercerts"

	"github.com/linkerd/linkerd2/controller/api/public"
	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	configPb "github.com/linkerd/linkerd2/controller/gen/config"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/identity"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1machinary "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sVersion "k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

// CategoryID is an identifier for the types of health checks.
type CategoryID string

const (
	// KubernetesAPIChecks adds a series of checks to validate that the caller is
	// configured to interact with a working Kubernetes cluster.
	KubernetesAPIChecks CategoryID = "kubernetes-api"

	// KubernetesVersionChecks validate that the cluster meets the minimum version
	// requirements.
	KubernetesVersionChecks CategoryID = "kubernetes-version"

	// LinkerdPreInstall* checks enabled by `linkerd check --pre`

	// LinkerdPreInstallChecks adds checks to validate that the control plane
	// namespace does not already exist, and that the user can create cluster-wide
	// resources, including ClusterRole, ClusterRoleBinding, and
	// CustomResourceDefinition, as well as namespace-wide resources, including
	// Service, Deployment, and ConfigMap. This check only runs as part of the set
	// of pre-install checks.
	// This check is dependent on the output of KubernetesAPIChecks, so those
	// checks must be added first.
	LinkerdPreInstallChecks CategoryID = "pre-kubernetes-setup"

	// LinkerdPreInstallCapabilityChecks adds a check to validate the user has the
	// capabilities necessary to deploy Linkerd. For example, the NET_ADMIN and
	// NET_RAW capabilities are required by the `linkerd-init` container to modify
	// IP tables. These checks are not run when the `--linkerd-cni-enabled` flag
	// is set.
	LinkerdPreInstallCapabilityChecks CategoryID = "pre-kubernetes-capability"

	// LinkerdPreInstallGlobalResourcesChecks adds a series of checks to determine
	// the existence of the global resources like cluster roles, cluster role
	// bindings, mutating webhook configuration validating webhook configuration
	// and name security policies during the pre-install phase. This check is used
	// to determine if a control plane is already installed.
	LinkerdPreInstallGlobalResourcesChecks CategoryID = "pre-linkerd-global-resources"

	// LinkerdConfigChecks enabled by `linkerd check config`

	// LinkerdConfigChecks adds a series of checks to validate that the Linkerd
	// namespace, RBAC, ServiceAccounts, and CRDs were successfully created.
	// These checks specifically validate that the `linkerd install config`
	// command succeeded in a multi-stage install, but also applies to a default
	// `linkerd install`.
	// These checks are dependent on the output of KubernetesAPIChecks, so those
	// checks must be added first.
	LinkerdConfigChecks CategoryID = "linkerd-config"

	// LinkerdControlPlaneExistenceChecks adds a series of checks to validate that
	// the control plane namespace and controller name exist.
	// These checks are dependent on the output of KubernetesAPIChecks, so those
	// checks must be added first.
	LinkerdControlPlaneExistenceChecks CategoryID = "linkerd-existence"

	// LinkerdAPIChecks adds a series of checks to validate that the control plane
	// is successfully serving the public API.
	// These checks are dependent on the output of KubernetesAPIChecks, so those
	// checks must be added first.
	LinkerdAPIChecks CategoryID = "linkerd-api"

	// LinkerdVersionChecks adds a series of checks to query for the latest
	// version, and validate the CLI is up to date.
	LinkerdVersionChecks CategoryID = "linkerd-version"

	// LinkerdControlPlaneVersionChecks adds a series of checks to validate that
	// the control plane is running the latest available version.
	// These checks are dependent on the following:
	// 1) `apiClient` from LinkerdControlPlaneExistenceChecks
	// 2) `latestVersions` from LinkerdVersionChecks
	// 3) `serverVersion` from `LinkerdControlPlaneExistenceChecks`
	LinkerdControlPlaneVersionChecks CategoryID = "control-plane-version"

	// LinkerdDataPlaneChecks adds data plane checks to validate that the data
	// plane namespace exists, and that the proxy containers are in a ready
	// state and running the latest available version.
	// These checks are dependent on the output of KubernetesAPIChecks,
	// `apiClient` from LinkerdControlPlaneExistenceChecks, and `latestVersions`
	// from LinkerdVersionChecks, so those checks must be added first.
	LinkerdDataPlaneChecks CategoryID = "linkerd-data-plane"

	// LinkerdIdentity Checks the integrity of the mTLS certificates
	// that the control plane is configured with
	LinkerdIdentity CategoryID = "linkerd-identity"

	// LinkerdIdentityDataPlane checks that integrity of the mTLS
	// certificates that the proxies are configured with and tries to
	// report useful information with respect to whether the configuration
	// is compatible with the one of the control plane
	LinkerdIdentityDataPlane CategoryID = "linkerd-identity-data-plane"

	// linkerdCniResourceLabel is the label key that is used to identify
	// whether a Kubernetes resource is related to the install-cni command
	// The value is expected to be "true", "false" or "", where "false" and
	// "" are equal, making "false" the default
	linkerdCniResourceLabel = "linkerd.io/cni-resource"
)

// HintBaseURL is the base URL on the linkerd.io website that all check hints
// point to. Each check adds its own `hintAnchor` to specify a location on the
// page.
const HintBaseURL = "https://linkerd.io/checks/#"

// AllowedClockSkew sets the allowed skew in clock synchronization
// between the system running inject command and the node(s), being
// based on assumed node's heartbeat interval (<= 60 seconds) plus default TLS
// clock skew allowance.
//
// TODO: Make this default value overridiable, e.g. by CLI flag
const AllowedClockSkew = time.Minute + tls.DefaultClockSkewAllowance

var (
	retryWindow    = 5 * time.Second
	requestTimeout = 30 * time.Second

	expectedServiceAccountNames = []string{
		"linkerd-controller",
		"linkerd-grafana",
		"linkerd-identity",
		"linkerd-prometheus",
		"linkerd-proxy-injector",
		"linkerd-sp-validator",
		"linkerd-web",
		"linkerd-tap",
	}
)

// Resource provides a way to describe a Kubernetes object, kind, and name.
// TODO: Consider sharing with the inject package's ResourceConfig.workload
// struct, as it wraps both runtime.Object and metav1.TypeMeta.
type Resource struct {
	groupVersionKind schema.GroupVersionKind
	name             string
}

// String outputs the resource in kind.group/name format, intended for
// `linkerd install`.
func (r *Resource) String() string {
	return fmt.Sprintf("%s/%s", strings.ToLower(r.groupVersionKind.GroupKind().String()), r.name)
}

// ResourceError provides a custom error type for resource existence checks,
// useful in printing detailed error messages in `linkerd check` and
// `linkerd install`.
type ResourceError struct {
	resourceName string
	Resources    []Resource
}

// Error satisfies the error interface for ResourceError. The output is intended
// for `linkerd check`.
func (e *ResourceError) Error() string {
	names := []string{}
	for _, res := range e.Resources {
		names = append(names, res.name)
	}
	return fmt.Sprintf("%s found but should not exist: %s", e.resourceName, strings.Join(names, " "))
}

// CategoryError provides a custom error type that also contains check category that emitted the error,
// useful when needed to distinguish between errors from multiple categories
type CategoryError struct {
	Category CategoryID
	Err      error
}

// Error satisfies the error interface for CategoryError.
func (e *CategoryError) Error() string {
	return e.Err.Error()
}

// IsCategoryError returns true if passed in error is of type CategoryError and belong to the given category
func IsCategoryError(err error, categoryID CategoryID) bool {
	if ce, ok := err.(*CategoryError); ok {
		return ce.Category == categoryID
	}
	return false
}

// CheckRunResult Represents the result of running an individual checker
type CheckRunResult struct {
	// Fatal indicates that all remaining checks should be aborted if this check
	// fails; it should only be used if subsequent checks cannot possibly succeed
	// (default false)
	Fatal bool

	// Warning indicates that if this check fails, it should be reported, but it
	// should not impact the overall outcome of the health check (default false)
	Warning bool

	// Err stores the error that the check has produced
	Err error
}

type checker struct {
	// description is the short description that's printed to the command line
	// when the check is executed
	description string

	// hintAnchor, when appended to `HintBaseURL`, provides a URL to more
	// information about the check
	hintAnchor string

	// retryDeadline establishes a deadline before which this check should be
	// retried; if the deadline has passed, the check fails (default: no retries)
	retryDeadline time.Time

	// surfaceErrorOnRetry indicates that the error message should be displayed
	// even if the check will be retried.  This is useful if the error message
	// contains the current status of the check.
	surfaceErrorOnRetry bool

	// check is the function that's called to execute the check; if the function
	// returns an error, the check fails
	check func(context.Context) CheckRunResult

	// checkRPC is an alternative to check that can be used to perform a remote
	// check using the SelfCheck gRPC endpoint; check status is based on the value
	// of the gRPC response
	checkRPC func(context.Context) (*healthcheckPb.SelfCheckResponse, CheckRunResult)
}

// CheckResult encapsulates a check's identifying information and output
// Note there exists an analogous user-facing type, `cmd.check`, for output via
// `linkerd check -o json`.
type CheckResult struct {
	Category    CategoryID
	Description string
	HintAnchor  string
	Retry       bool
	Warning     bool
	Err         error
}

// CheckObserver receives the results of each check.
type CheckObserver func(*CheckResult)

type category struct {
	id       CategoryID
	checkers []checker
	enabled  bool
}

// Options specifies configuration for a HealthChecker.
type Options struct {
	ControlPlaneNamespace string
	DataPlaneNamespace    string
	KubeConfig            string
	KubeContext           string
	Impersonate           string
	APIAddr               string
	VersionOverride       string
	RetryDeadline         time.Time
	NoInitContainer       bool
}

// HealthChecker encapsulates all health check checkers, and clients required to
// perform those checks.
type HealthChecker struct {
	categories []category
	*Options

	// these fields are set in the process of running checks
	kubeAPI          *k8s.KubernetesAPI
	kubeVersion      *k8sVersion.Info
	controlPlanePods []corev1.Pod
	apiClient        public.APIClient
	latestVersions   version.Channels
	serverVersion    string
	linkerdConfig    *configPb.All
	uuid             string
	issuerCert       *tls.Cred
	roots            []*x509.Certificate
	meshedPods       []meshedPodData
	recordEvent      func(eventType, reason, message string)
}

// NewHealthChecker returns an initialized HealthChecker
func NewHealthChecker(categoryIDs []CategoryID, options *Options) *HealthChecker {
	hc := &HealthChecker{
		Options: options,
	}

	hc.categories = hc.allCategories()

	checkMap := map[CategoryID]struct{}{}
	for _, category := range categoryIDs {
		checkMap[category] = struct{}{}
	}
	for i := range hc.categories {
		if _, ok := checkMap[hc.categories[i].id]; ok {
			hc.categories[i].enabled = true
		}
	}

	return hc
}

// allCategories is the global, ordered list of all checkers, grouped by
// category. This method is attached to the HealthChecker struct because the
// checkers directly reference other members of the struct, such as kubeAPI,
// controlPlanePods, etc.
//
// Ordering is important because checks rely on specific `HealthChecker` members
// getting populated by earlier checks, such as kubeAPI, controlPlanePods, etc.
//
// Note that all checks should include a `hintAnchor` with a corresponding section
// in the linkerd check faq:
// https://linkerd.io/checks/#
func (hc *HealthChecker) allCategories() []category {
	return []category{
		{
			id: KubernetesAPIChecks,
			checkers: []checker{
				{
					description: "can initialize the client",
					hintAnchor:  "k8s-api",
					check: func(context.Context) CheckRunResult {
						api, err := k8s.NewAPI(hc.KubeConfig, hc.KubeContext, hc.Impersonate, requestTimeout)
						hc.kubeAPI = api
						return CheckRunResult{Fatal: true, Err: err}
					},
				},
				{
					description: "can query the Kubernetes API",
					hintAnchor:  "k8s-api",
					check: func(context.Context) CheckRunResult {
						ver, err := hc.kubeAPI.GetVersionInfo()
						hc.kubeVersion = ver
						return CheckRunResult{Fatal: true, Err: err}
					},
				},
			},
		},
		{
			id: KubernetesVersionChecks,
			checkers: []checker{
				{
					description: "is running the minimum Kubernetes API version",
					hintAnchor:  "k8s-version",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.kubeAPI.CheckVersion(hc.kubeVersion)}
					},
				},
				{
					description: "is running the minimum kubectl version",
					hintAnchor:  "kubectl-version",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: k8s.CheckKubectlVersion()}
					},
				},
			},
		},
		{
			id: LinkerdPreInstallChecks,
			checkers: []checker{
				{
					description: "control plane namespace does not already exist",
					hintAnchor:  "pre-ns",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkNamespace(hc.ControlPlaneNamespace, false)}
					},
				},
				{
					description: "can create Namespaces",
					hintAnchor:  "pre-k8s-cluster-k8s",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkCanCreate("", "", "v1", "namespaces")}
					},
				},
				{
					description: "can create ClusterRoles",
					hintAnchor:  "pre-k8s-cluster-k8s",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkCanCreate("", "rbac.authorization.k8s.io", "v1beta1", "clusterroles")}
					},
				},
				{
					description: "can create ClusterRoleBindings",
					hintAnchor:  "pre-k8s-cluster-k8s",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkCanCreate("", "rbac.authorization.k8s.io", "v1beta1", "clusterrolebindings")}
					},
				},
				{
					description: "can create CustomResourceDefinitions",
					hintAnchor:  "pre-k8s-cluster-k8s",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkCanCreate("", "apiextensions.k8s.io", "v1beta1", "customresourcedefinitions")}
					},
				},
				{
					description: "can create PodSecurityPolicies",
					hintAnchor:  "pre-k8s",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkCanCreate(hc.ControlPlaneNamespace, "policy", "v1beta1", "podsecuritypolicies")}
					},
				},
				{
					description: "can create ServiceAccounts",
					hintAnchor:  "pre-k8s",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkCanCreate(hc.ControlPlaneNamespace, "", "v1", "serviceaccounts")}
					},
				},
				{
					description: "can create Services",
					hintAnchor:  "pre-k8s",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkCanCreate(hc.ControlPlaneNamespace, "", "v1", "services")}
					},
				},
				{
					description: "can create Deployments",
					hintAnchor:  "pre-k8s",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkCanCreate(hc.ControlPlaneNamespace, "apps", "v1", "deployments")}
					},
				},
				{
					description: "can create CronJobs",
					hintAnchor:  "pre-k8s",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkCanCreate(hc.ControlPlaneNamespace, "batch", "v1beta1", "cronjobs")}
					},
				},
				{
					description: "can create ConfigMaps",
					hintAnchor:  "pre-k8s",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkCanCreate(hc.ControlPlaneNamespace, "", "v1", "configmaps")}
					},
				},
				{
					description: "can create Secrets",
					hintAnchor:  "pre-k8s",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkCanCreate(hc.ControlPlaneNamespace, "", "v1", "secrets")}
					},
				},
				{
					description: "can read Secrets",
					hintAnchor:  "pre-k8s",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkCanGet(hc.ControlPlaneNamespace, "", "v1", "secrets")}
					},
				},
				{
					description: "no clock skew detected",
					hintAnchor:  "pre-k8s-clock-skew",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkClockSkew()}
					},
				},
			},
		},
		{
			id: LinkerdPreInstallCapabilityChecks,
			checkers: []checker{
				{
					description: "has NET_ADMIN capability",
					hintAnchor:  "pre-k8s-cluster-net-admin",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkCapability("NET_ADMIN"), Warning: true}
					},
				},
				{
					description: "has NET_RAW capability",
					hintAnchor:  "pre-k8s-cluster-net-raw",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkCapability("NET_RAW"), Warning: true}
					},
				},
			},
		},
		{
			id: LinkerdPreInstallGlobalResourcesChecks,
			checkers: []checker{
				{
					description: "no ClusterRoles exist",
					hintAnchor:  "pre-l5d-existence",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkClusterRoles(false)}
					},
				},
				{
					description: "no ClusterRoleBindings exist",
					hintAnchor:  "pre-l5d-existence",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkClusterRoleBindings(false)}
					},
				},
				{
					description: "no CustomResourceDefinitions exist",
					hintAnchor:  "pre-l5d-existence",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkCustomResourceDefinitions(false)}
					},
				},
				{
					description: "no MutatingWebhookConfigurations exist",
					hintAnchor:  "pre-l5d-existence",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkMutatingWebhookConfigurations(false)}
					},
				},
				{
					description: "no ValidatingWebhookConfigurations exist",
					hintAnchor:  "pre-l5d-existence",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkValidatingWebhookConfigurations(false)}
					},
				},
				{
					description: "no PodSecurityPolicies exist",
					hintAnchor:  "pre-l5d-existence",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkPodSecurityPolicies(false)}
					},
				},
			},
		},
		{
			id: LinkerdConfigChecks,
			checkers: []checker{
				{
					description: "control plane Namespace exists",
					hintAnchor:  "l5d-existence-ns",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkNamespace(hc.ControlPlaneNamespace, true), Fatal: true}
					},
				},
				{
					description: "control plane ClusterRoles exist",
					hintAnchor:  "l5d-existence-cr",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkClusterRoles(true), Fatal: true}
					},
				},
				{
					description: "control plane ClusterRoleBindings exist",
					hintAnchor:  "l5d-existence-crb",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkClusterRoleBindings(true), Fatal: true}
					},
				},
				{
					description: "control plane ServiceAccounts exist",
					hintAnchor:  "l5d-existence-sa",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkServiceAccounts(expectedServiceAccountNames), Fatal: true}
					},
				},
				{
					description: "control plane CustomResourceDefinitions exist",
					hintAnchor:  "l5d-existence-crd",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkCustomResourceDefinitions(true), Fatal: true}
					},
				},
				{
					description: "control plane MutatingWebhookConfigurations exist",
					hintAnchor:  "l5d-existence-mwc",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkMutatingWebhookConfigurations(true), Fatal: true}
					},
				},
				{
					description: "control plane ValidatingWebhookConfigurations exist",
					hintAnchor:  "l5d-existence-vwc",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkValidatingWebhookConfigurations(true), Fatal: true}

					},
				},
				{
					description: "control plane PodSecurityPolicies exist",
					hintAnchor:  "l5d-existence-psp",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Err: hc.checkPodSecurityPolicies(true), Fatal: true}
					},
				},
			},
		},
		{
			id: LinkerdControlPlaneExistenceChecks,
			checkers: []checker{
				{
					description: "'linkerd-config' config map exists",
					hintAnchor:  "l5d-existence-linkerd-config",
					check: func(context.Context) CheckRunResult {
						uuid, linkerdConfig, err := hc.checkLinkerdConfigConfigMap()
						hc.uuid = uuid
						hc.linkerdConfig = linkerdConfig
						return CheckRunResult{Err: err, Fatal: true}
					},
				},
				{
					description: "heartbeat ServiceAccount exist",
					hintAnchor:  "l5d-existence-sa",
					check: func(context.Context) CheckRunResult {
						if hc.isHeartbeatDisabled() {
							return CheckRunResult{}
						}
						return CheckRunResult{Err: hc.checkServiceAccounts([]string{"linkerd-heartbeat"}), Fatal: true}
					},
				},
				{
					description: "control plane replica sets are ready",
					hintAnchor:  "l5d-existence-replicasets",
					check: func(context.Context) CheckRunResult {
						controlPlaneReplicaSet, err := hc.kubeAPI.GetReplicaSets(hc.ControlPlaneNamespace)
						if err != nil {
							return CheckRunResult{Err: err, Fatal: true}
						}
						return CheckRunResult{Err: checkControlPlaneReplicaSets(controlPlaneReplicaSet), Fatal: true}
					},
				},
				{
					description: "no unschedulable pods",
					hintAnchor:  "l5d-existence-unschedulable-pods",
					check: func(context.Context) CheckRunResult {
						// do not save this into hc.controlPlanePods, as this check may
						// succeed prior to all expected control plane pods being up
						controlPlanePods, err := hc.kubeAPI.GetPodsByNamespace(hc.ControlPlaneNamespace)
						if err != nil {
							return CheckRunResult{Err: err, Fatal: true}
						}
						return CheckRunResult{Err: checkUnschedulablePods(controlPlanePods), Fatal: true}

					},
				},
				{
					description:         "controller pod is running",
					hintAnchor:          "l5d-existence-controller",
					retryDeadline:       hc.RetryDeadline,
					surfaceErrorOnRetry: true,
					check: func(context.Context) CheckRunResult {
						// save this into hc.controlPlanePods, since this check only
						// succeeds when all pods are up
						var err error
						hc.controlPlanePods, err = hc.kubeAPI.GetPodsByNamespace(hc.ControlPlaneNamespace)
						if err != nil {
							return CheckRunResult{Err: err, Fatal: true}
						}
						return CheckRunResult{Err: checkControllerRunning(hc.controlPlanePods), Fatal: true}
					},
				},
				{
					description: "can initialize the client",
					hintAnchor:  "l5d-existence-client",
					check: func(context.Context) CheckRunResult {
						result := CheckRunResult{Fatal: true}
						if hc.APIAddr != "" {
							hc.apiClient, result.Err = public.NewInternalClient(hc.ControlPlaneNamespace, hc.APIAddr)
						} else {
							hc.apiClient, result.Err = public.NewExternalClient(hc.ControlPlaneNamespace, hc.kubeAPI)
						}
						return result
					},
				},
				{
					description:   "can query the control plane API",
					hintAnchor:    "l5d-existence-api",
					retryDeadline: hc.RetryDeadline,
					check: func(ctx context.Context) CheckRunResult {
						r := CheckRunResult{Fatal: true}
						hc.serverVersion, r.Err = GetServerVersion(ctx, hc.apiClient)
						return r
					},
				},
			},
		},
		{
			id: LinkerdIdentity,
			checkers: []checker{
				{
					description: "certificate config is valid",
					hintAnchor:  "l5d-crt-config-is-valid",
					check: func(context.Context) CheckRunResult {
						r := CheckRunResult{Fatal: true}
						hc.issuerCert, hc.roots, r.Err = hc.checkCertificatesConfig()
						return r
					},
				},
				{
					description: "control plane is using supported trust roots",
					hintAnchor:  "l5d-supported-certs-type",
					check: func(context.Context) CheckRunResult {
						var invalidRoots []string
						for _, root := range hc.roots {
							if err := issuercerts.CheckCertAlgoRequirements(root); err != nil {
								invalidRoots = append(invalidRoots, fmt.Sprintf("    %v %s %s", root.SerialNumber, root.Subject.CommonName, err))
							}
						}
						if len(invalidRoots) > 0 {
							return CheckRunResult{Fatal: true, Err: fmt.Errorf("Invalid roots:\n%s", strings.Join(invalidRoots, "\n"))}

						}
						return CheckRunResult{}
					},
				},
				{
					description: "identity is using supported issuer certificate",
					hintAnchor:  "l5d-supported-certs-type",
					check: func(context.Context) CheckRunResult {
						if err := issuercerts.CheckCertAlgoRequirements(hc.issuerCert.Certificate); err != nil {
							return CheckRunResult{Fatal: true, Err: fmt.Errorf("issuer certificate %s", err)}
						}
						return CheckRunResult{}
					},
				},
				{
					description: "all control plane trust roots have valid lifetimes",
					hintAnchor:  "l5d-certs-rotation",
					check: func(ctx context.Context) CheckRunResult {
						var invalidRoots []string
						var expiringRoots []string
						rootInfoFormat := "    %v %s %s"
						for _, root := range hc.roots {
							if err := issuercerts.CheckCertTimeValidity(root); err != nil {
								invalidRoots = append(invalidRoots, fmt.Sprintf(rootInfoFormat, root.SerialNumber, root.Subject.CommonName, err))
							}
							if err := issuercerts.CheckExpiringSoon(root); err != nil {
								expiringRoots = append(expiringRoots, fmt.Sprintf(rootInfoFormat, root.SerialNumber, root.Subject.CommonName, err))
							}
						}

						if len(invalidRoots) > 0 {
							return CheckRunResult{Warning: false, Fatal: true, Err: fmt.Errorf("Invalid roots:\n%s", strings.Join(invalidRoots, "\n"))}
						}

						if len(expiringRoots) > 0 {
							if recorder, err := hc.eventRecorder(); err == nil {
								recorder(corev1.EventTypeWarning, "LinkerdCertsExpiringSoon", "Some of the TLS certificates that Linkerd is configured with are expiring soon")
							}
							return CheckRunResult{Warning: true, Fatal: false, Err: fmt.Errorf("Roots expiring soon:\n%s", strings.Join(expiringRoots, "\n"))}

						}
						return CheckRunResult{}
					},
				},
				{
					description: "issuer cert has valid lifetime",
					hintAnchor:  "l5d-certs-rotation",
					check: func(ctx context.Context) CheckRunResult {
						if err := issuercerts.CheckCertTimeValidity(hc.issuerCert.Certificate); err != nil {
							return CheckRunResult{Warning: false, Fatal: true, Err: fmt.Errorf("issuer certiifcate is %s", err)}
						}
						if err := issuercerts.CheckExpiringSoon(hc.issuerCert.Certificate); err != nil {
							if recorder, err := hc.eventRecorder(); err == nil {
								recorder(corev1.EventTypeWarning, "LinkerdCertsExpiringSoon", "Some of the TLS certificates that Linkerd is configured with are expiring soon")
							}
							return CheckRunResult{Warning: true, Fatal: false, Err: fmt.Errorf("issuer certiifcate %s", err)}
						}
						return CheckRunResult{}
					},
				},
				{
					description: "issuer cert can be verified with trust root",
					hintAnchor:  "l5d-certs-rotation",
					check: func(ctx context.Context) CheckRunResult {
						if err := hc.issuerCert.Verify(tls.CertificatesToPool(hc.roots), hc.issuerDNS()); err != nil {
							return CheckRunResult{Err: err, Fatal: true}
						}
						return CheckRunResult{}
					},
				},
			},
		},
		{
			id: LinkerdIdentityDataPlane,
			checkers: []checker{
				{
					description: "data plane is using supported trust roots",
					hintAnchor:  "l5d-supported-certs-type",
					check: func(context.Context) CheckRunResult {
						pods, err := hc.getMeshedPods()
						if err != nil {
							return CheckRunResult{Fatal: true, Err: err}
						}
						hc.meshedPods = pods
						var invalidRoots []string
						affectedPods := make(map[string]bool)

						unparsableErrorFmt := "      * Unparsable trustRoots for name %s"
						invalidRootFormat := "      * %v %s %s in name %s"
						errorFormat := "Invalid roots:\n%s\n    Affected pods: %d/%d"

						for _, pod := range hc.meshedPods {
							if pod.trustRoots == nil {
								invalidRoots = append(invalidRoots, fmt.Sprintf(unparsableErrorFmt, pod.name))
								affectedPods[pod.name] = true
								continue
							}
							for _, root := range pod.trustRoots {
								if err := issuercerts.CheckCertAlgoRequirements(root); err != nil {
									invalidRoots = append(invalidRoots, fmt.Sprintf(invalidRootFormat, root.SerialNumber, root.Subject.CommonName, err, pod.name))
									affectedPods[pod.name] = true
								}
							}
						}
						if len(invalidRoots) > 0 {
							return CheckRunResult{Fatal: true, Err: fmt.Errorf(errorFormat, strings.Join(invalidRoots, "\n"), len(affectedPods), len(hc.meshedPods))}
						}
						return CheckRunResult{}
					},
				},
				{
					description: "all data plane trust roots have valid lifetimes",
					hintAnchor:  "l5d-certs-rotation",
					check: func(context.Context) CheckRunResult {
						expiredRoots := make(map[string]bool)
						podsAffectedByExpiredRoots := make(map[string]bool)
						expiringRoots := make(map[string]bool)
						podsAffectedByExpiringRoots := make(map[string]bool)

						rootInfoFormat := "      * %v %s %s in pod %s"
						expiredRootsErrorFormat := "Expired roots:\n%s\n    Affected pods: %d/%d"
						expiringRootsErrorFormat := "Roots expiring soon:\n%s\n    Affected pods: %d/%d"

						for _, pod := range hc.meshedPods {
							if pod.trustRoots != nil {
								for _, root := range pod.trustRoots {
									if err := issuercerts.CheckCertTimeValidity(root); err != nil {
										expiredRoots[fmt.Sprintf(rootInfoFormat, root.SerialNumber, root.Subject.CommonName, err, pod.name)] = true
										podsAffectedByExpiredRoots[pod.name] = true
									}

									if err := issuercerts.CheckExpiringSoon(root); err != nil {
										expiringRoots[fmt.Sprintf(rootInfoFormat, root.SerialNumber, root.Subject.CommonName, err, pod.name)] = true
										podsAffectedByExpiringRoots[pod.name] = true
									}
								}
							}
						}
						if len(expiredRoots) > 0 {
							expiredRoots := strings.Join(sortedKeys(expiredRoots), "\n")
							return CheckRunResult{Warning: false, Fatal: true, Err: fmt.Errorf(expiredRootsErrorFormat, expiredRoots, len(podsAffectedByExpiredRoots), len(hc.meshedPods))}
						}

						if len(expiringRoots) > 0 {
							if recorder, err := hc.eventRecorder(); err == nil {
								recorder(corev1.EventTypeWarning, "LinkerdCertsExpiringSoon", "Some of the TLS certificates that Linkerd is configured with are expiring soon")
							}
							rootsToExpireSooon := strings.Join(sortedKeys(expiringRoots), "\n")
							return CheckRunResult{Warning: true, Fatal: false, Err: fmt.Errorf(expiringRootsErrorFormat, rootsToExpireSooon, len(podsAffectedByExpiringRoots), len(hc.meshedPods))}

						}
						return CheckRunResult{}

					},
				},
				{
					description: "control plane issuer certificate can be verified with data plane trust roots",
					hintAnchor:  "l5d-certs-rotation",
					check: func(ctx context.Context) CheckRunResult {
						var affectedPods []string
						errorFormat := "Roots in pods cannot verify control plane issuer cert:\n%s\n    Affected pods: %d/%d"
						for _, pod := range hc.meshedPods {
							if err := hc.issuerCert.Verify(tls.CertificatesToPool(pod.trustRoots), hc.issuerDNS()); err != nil {
								affectedPods = append(affectedPods, fmt.Sprintf("      * %s", pod.name))
							}
						}
						if len(affectedPods) > 0 {
							affectedPods = affectedPods[:]
							affected := strings.Join(affectedPods, "\n")
							return CheckRunResult{Fatal: true, Err: fmt.Errorf(errorFormat, affected, len(affectedPods), len(hc.meshedPods))}
						}
						return CheckRunResult{}
					},
				},
			},
		},
		{
			id: LinkerdAPIChecks,
			checkers: []checker{
				{
					description:         "control plane pods are ready",
					hintAnchor:          "l5d-api-control-ready",
					retryDeadline:       hc.RetryDeadline,
					surfaceErrorOnRetry: true,
					check: func(context.Context) CheckRunResult {
						var err error
						hc.controlPlanePods, err = hc.kubeAPI.GetPodsByNamespace(hc.ControlPlaneNamespace)
						if err != nil {
							return CheckRunResult{Fatal: true, Err: err}
						}
						return CheckRunResult{Fatal: true, Err: validateControlPlanePods(hc.controlPlanePods)}
					},
				},
				{
					description:   "control plane self-check",
					hintAnchor:    "l5d-api-control-api",
					retryDeadline: hc.RetryDeadline,
					checkRPC: func(ctx context.Context) (*healthcheckPb.SelfCheckResponse, CheckRunResult) {
						resp, err := hc.apiClient.SelfCheck(ctx, &healthcheckPb.SelfCheckRequest{})
						return resp, CheckRunResult{Fatal: true, Err: err}
					},
				},
			},
		},
		{
			id: LinkerdVersionChecks,
			checkers: []checker{
				{
					description: "can determine the latest version",
					hintAnchor:  "l5d-version-latest",
					check: func(ctx context.Context) CheckRunResult {
						var err error
						if hc.VersionOverride != "" {
							hc.latestVersions, err = version.NewChannels(hc.VersionOverride)
						} else {
							uuid := "unknown"
							if hc.uuid != "" {
								uuid = hc.uuid
							}
							hc.latestVersions, err = version.GetLatestVersions(ctx, uuid, "cli")
						}
						return CheckRunResult{Err: err}
					},
				},
				{
					description: "cli is up-to-date",
					hintAnchor:  "l5d-version-cli",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Warning: true, Err: hc.latestVersions.Match(version.Version)}
					},
				},
			},
		},
		{
			id: LinkerdControlPlaneVersionChecks,
			checkers: []checker{
				{
					description: "control plane is up-to-date",
					hintAnchor:  "l5d-version-control",
					check: func(context.Context) CheckRunResult {
						return CheckRunResult{Warning: true, Err: hc.latestVersions.Match(hc.serverVersion)}
					},
				},
				{
					description: "control plane and cli versions match",
					hintAnchor:  "l5d-version-control",
					check: func(context.Context) CheckRunResult {
						if hc.serverVersion != version.Version {
							return CheckRunResult{Warning: true, Err: fmt.Errorf("control plane running %s but cli running %s", hc.serverVersion, version.Version)}
						}
						return CheckRunResult{}
					},
				},
			},
		},
		{
			id: LinkerdDataPlaneChecks,
			checkers: []checker{
				{
					description: "data plane namespace exists",
					hintAnchor:  "l5d-data-plane-exists",
					check: func(context.Context) CheckRunResult {
						if hc.DataPlaneNamespace == "" {
							// when checking proxies in all namespaces, this check is a no-op
							return CheckRunResult{}
						}
						return CheckRunResult{Fatal: true, Err: hc.checkNamespace(hc.DataPlaneNamespace, true)}
					},
				},
				{
					description:   "data plane proxies are ready",
					hintAnchor:    "l5d-data-plane-ready",
					retryDeadline: hc.RetryDeadline,
					check: func(ctx context.Context) CheckRunResult {
						pods, err := hc.getDataPlanePods(ctx)
						if err != nil {
							return CheckRunResult{Fatal: true, Err: err}
						}

						return CheckRunResult{Fatal: true, Err: validateDataPlanePods(pods, hc.DataPlaneNamespace)}
					},
				},
				{
					description:   "data plane proxy metrics are present in Prometheus",
					hintAnchor:    "l5d-data-plane-prom",
					retryDeadline: hc.RetryDeadline,
					check: func(ctx context.Context) CheckRunResult {
						pods, err := hc.getDataPlanePods(ctx)
						if err != nil {
							return CheckRunResult{Err: err}
						}

						return CheckRunResult{Err: validateDataPlanePodReporting(pods)}
					},
				},
				{
					description: "data plane is up-to-date",
					hintAnchor:  "l5d-data-plane-version",
					check: func(ctx context.Context) CheckRunResult {
						pods, err := hc.getDataPlanePods(ctx)
						if err != nil {
							return CheckRunResult{Warning: true, Err: err}
						}

						for _, pod := range pods {
							err = hc.latestVersions.Match(pod.ProxyVersion)
							if err != nil {
								return CheckRunResult{Warning: true, Err: fmt.Errorf("%s: %s", pod.Name, err)}
							}
						}
						return CheckRunResult{}
					},
				},
				{
					description: "data plane and cli versions match",
					hintAnchor:  "l5d-data-plane-cli-version",
					check: func(ctx context.Context) CheckRunResult {
						pods, err := hc.getDataPlanePods(ctx)
						if err != nil {
							return CheckRunResult{Warning: true, Err: err}
						}

						for _, pod := range pods {
							if pod.ProxyVersion != version.Version {
								return CheckRunResult{Warning: true, Err: fmt.Errorf("%s running %s but cli running %s", pod.Name, pod.ProxyVersion, version.Version)}
							}
						}
						return CheckRunResult{}
					},
				},
			},
		},
	}
}

func (hc *HealthChecker) issuerDNS() string {
	return fmt.Sprintf("identity.%s.%s", hc.ControlPlaneNamespace, hc.linkerdConfig.Global.IdentityContext.TrustDomain)
}

func sortedKeys(m map[string]bool) []string {
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

// Add adds an arbitrary checker. This should only be used for testing. For
// production code, pass in the desired set of checks when calling
// NewHealthChecker.
func (hc *HealthChecker) Add(categoryID CategoryID, description string, hintAnchor string, check func(context.Context) CheckRunResult) {
	hc.addCategory(
		category{
			id: categoryID,
			checkers: []checker{
				{
					description: description,
					check:       check,
					hintAnchor:  hintAnchor,
				},
			},
		},
	)
}

// addCategory is also for testing
func (hc *HealthChecker) addCategory(c category) {
	c.enabled = true
	hc.categories = append(hc.categories, c)
}

// Create K8s event recorder
func (hc *HealthChecker) eventRecorder() (func(eventType, reason, message string), error) {
	if hc.recordEvent != nil {
		return hc.recordEvent, nil
	}

	if hc.kubeAPI == nil {
		return nil, errors.New("K8s api not initialized yet")
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: hc.kubeAPI.CoreV1().Events(hc.ControlPlaneNamespace),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "linkerd-identity"})
	deployment, err := hc.kubeAPI.AppsV1().Deployments(hc.ControlPlaneNamespace).Get("linkerd-identity", v1machinary.GetOptions{})
	if err != nil {
		return nil, err
	}

	recordEventFunc := func(eventType, reason, message string) {
		recorder.Event(deployment, eventType, reason, message)
	}

	hc.recordEvent = recordEventFunc
	return hc.recordEvent, nil
}

// RunChecks runs all configured checkers, and passes the results of each
// check to the observer. If a check fails and is marked as Fatal, then all
// remaining checks are skipped. If at least one check fails, RunChecks returns
// false; if all checks passed, RunChecks returns true.  Checks which are
// designated as warnings will not cause RunCheck to return false, however.
func (hc *HealthChecker) RunChecks(observer CheckObserver) bool {
	success := true

	for _, c := range hc.categories {
		if c.enabled {
			for _, checker := range c.checkers {
				checker := checker // pin
				if checker.check != nil {
					r := hc.runCheck(c.id, &checker, observer)
					if r.Err != nil {
						if !r.Warning {
							success = false
						}
						if r.Fatal {
							return success
						}
					}
				}

				if checker.checkRPC != nil {
					r := hc.runCheckRPC(c.id, &checker, observer)
					if r.Err != nil {
						if !r.Warning {
							success = false
						}
						if r.Fatal {
							return success
						}
					}
				}
			}
		}
	}

	return success
}

func (hc *HealthChecker) runCheck(categoryID CategoryID, c *checker, observer CheckObserver) CheckRunResult {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		res := c.check(ctx)
		e := res.Err
		if e != nil {
			res.Err = &CategoryError{categoryID, e}
		}
		checkResult := &CheckResult{
			Category:    categoryID,
			Description: c.description,
			HintAnchor:  c.hintAnchor,
			Warning:     res.Warning,
			Err:         res.Err,
		}

		if res.Err != nil && time.Now().Before(c.retryDeadline) {
			checkResult.Retry = true
			if !c.surfaceErrorOnRetry {
				checkResult.Err = errors.New("waiting for check to complete")
			}
			log.Debugf("Retrying on error: %s", res.Err)

			observer(checkResult)
			time.Sleep(retryWindow)
			continue
		}

		observer(checkResult)
		return res
	}
}

func (hc *HealthChecker) runCheckRPC(categoryID CategoryID, c *checker, observer CheckObserver) CheckRunResult {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	checkRsp, res := c.checkRPC(ctx)
	e := res.Err
	if e != nil {
		res.Err = &CategoryError{categoryID, e}
	}
	observer(&CheckResult{
		Category:    categoryID,
		Description: c.description,
		HintAnchor:  c.hintAnchor,
		Warning:     res.Warning,
		Err:         res.Err,
	})
	if res.Err != nil {
		return res
	}

	for _, check := range checkRsp.Results {
		var err error
		if check.Status != healthcheckPb.CheckStatus_OK {
			err = fmt.Errorf(check.FriendlyMessageToUser)
		}
		if err != nil {
			err = &CategoryError{categoryID, err}
		}
		observer(&CheckResult{
			Category:    categoryID,
			Description: fmt.Sprintf("[%s] %s", check.SubsystemName, check.CheckDescription),
			HintAnchor:  c.hintAnchor,
			Warning:     res.Warning,
			Err:         err,
		})
		if err != nil {
			res.Err = err
			return res
		}
	}

	return CheckRunResult{}
}

// PublicAPIClient returns a fully configured public API client. This client is
// only configured if the KubernetesAPIChecks and LinkerdAPIChecks are
// configured and run first.
func (hc *HealthChecker) PublicAPIClient() public.APIClient {
	return hc.apiClient
}

func (hc *HealthChecker) checkLinkerdConfigConfigMap() (string, *configPb.All, error) {
	cm, configPB, err := FetchLinkerdConfigMap(hc.kubeAPI, hc.ControlPlaneNamespace)
	if err != nil {
		return "", nil, err
	}

	return string(cm.GetUID()), configPB, nil
}

func (hc *HealthChecker) checkCertificatesConfig() (*tls.Cred, []*x509.Certificate, error) {
	_, configPB, err := FetchLinkerdConfigMap(hc.kubeAPI, hc.ControlPlaneNamespace)
	if err != nil {
		return nil, nil, err
	}

	idctx := configPB.Global.IdentityContext
	var data *issuercerts.IssuerCertData

	if idctx.Scheme == "" || idctx.Scheme == k8s.IdentityIssuerSchemeLinkerd {
		data, err = issuercerts.FetchIssuerData(hc.kubeAPI, idctx.TrustAnchorsPem, hc.ControlPlaneNamespace)
	} else {
		data, err = issuercerts.FetchExternalIssuerData(hc.kubeAPI, hc.ControlPlaneNamespace)
		// ensure trust trustRoots in config matches whats in the secret
		if data != nil && idctx.TrustAnchorsPem != data.TrustAnchors {
			errFormat := "IdentityContext.TrustAnchorsPem does not match %s in %s"
			err = fmt.Errorf(errFormat, k8s.IdentityIssuerTrustAnchorsNameExternal, k8s.IdentityIssuerSecretName)
		}
	}

	if err != nil {
		return nil, nil, err
	}

	issuerCreds, err := tls.ValidateAndCreateCreds(data.IssuerCrt, data.IssuerKey)
	if err != nil {
		return nil, nil, err
	}

	roots, err := tls.DecodePEMCertificates(data.TrustAnchors)
	if err != nil {
		return nil, nil, err
	}

	return issuerCreds, roots, nil
}

// FetchLinkerdConfigMap retrieves the `linkerd-config` ConfigMap from
// Kubernetes and parses it into `linkerd2.config` protobuf.
// TODO: Consider a different package for this function. This lives in the
// healthcheck package because healthcheck depends on it, along with other
// packages that also depend on healthcheck. This function depends on both
// `pkg/k8s` and `pkg/config`, which do not depend on each other.
func FetchLinkerdConfigMap(k kubernetes.Interface, controlPlaneNamespace string) (*corev1.ConfigMap, *configPb.All, error) {
	cm, err := k.CoreV1().ConfigMaps(controlPlaneNamespace).Get(k8s.ConfigConfigMapName, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}

	configPB, err := config.FromConfigMap(cm.Data)
	if err != nil {
		return nil, nil, err
	}

	return cm, configPB, nil
}

// checkNamespace checks whether the given namespace exists, and returns an
// error if it does not match `shouldExist`.
func (hc *HealthChecker) checkNamespace(namespace string, shouldExist bool) error {
	exists, err := hc.kubeAPI.NamespaceExists(namespace)
	if err != nil {
		return err
	}
	if shouldExist && !exists {
		return fmt.Errorf("The \"%s\" namespace does not exist", namespace)
	}
	if !shouldExist && exists {
		return fmt.Errorf("The \"%s\" namespace already exists", namespace)
	}
	return nil
}

func (hc *HealthChecker) expectedRBACNames() []string {
	return []string{
		fmt.Sprintf("linkerd-%s-controller", hc.ControlPlaneNamespace),
		fmt.Sprintf("linkerd-%s-identity", hc.ControlPlaneNamespace),
		fmt.Sprintf("linkerd-%s-prometheus", hc.ControlPlaneNamespace),
		fmt.Sprintf("linkerd-%s-proxy-injector", hc.ControlPlaneNamespace),
		fmt.Sprintf("linkerd-%s-sp-validator", hc.ControlPlaneNamespace),
		fmt.Sprintf("linkerd-%s-tap", hc.ControlPlaneNamespace),
	}
}

func (hc *HealthChecker) checkClusterRoles(shouldExist bool) error {
	options := metav1.ListOptions{
		LabelSelector: k8s.ControllerNSLabel,
	}
	crList, err := hc.kubeAPI.RbacV1().ClusterRoles().List(options)
	if err != nil {
		return err
	}

	objects := []runtime.Object{}

	for _, item := range crList.Items {

		if hc.skipNoInitContainerResources(item.ObjectMeta.Labels) {
			continue
		}
		item := item // pin
		objects = append(objects, &item)
	}

	return checkResources("ClusterRoles", objects, hc.expectedRBACNames(), shouldExist)
}

func (hc *HealthChecker) checkClusterRoleBindings(shouldExist bool) error {
	options := metav1.ListOptions{
		LabelSelector: k8s.ControllerNSLabel,
	}
	crbList, err := hc.kubeAPI.RbacV1().ClusterRoleBindings().List(options)
	if err != nil {
		return err
	}

	objects := []runtime.Object{}

	for _, item := range crbList.Items {
		if hc.skipNoInitContainerResources(item.ObjectMeta.Labels) {
			continue
		}

		item := item // pin
		objects = append(objects, &item)
	}

	return checkResources("ClusterRoleBindings", objects, hc.expectedRBACNames(), shouldExist)
}

func (hc *HealthChecker) isHeartbeatDisabled() bool {
	for _, flag := range hc.linkerdConfig.GetInstall().GetFlags() {
		if flag.GetName() == "disable-heartbeat" && flag.GetValue() == "true" {
			return true
		}
	}
	return false
}

func (hc *HealthChecker) checkServiceAccounts(saNames []string) error {
	options := metav1.ListOptions{
		LabelSelector: k8s.ControllerNSLabel,
	}
	saList, err := hc.kubeAPI.CoreV1().ServiceAccounts(hc.ControlPlaneNamespace).List(options)
	if err != nil {
		return err
	}

	objects := []runtime.Object{}
	for _, item := range saList.Items {
		item := item // pin
		objects = append(objects, &item)
	}

	return checkResources("ServiceAccounts", objects, saNames, true)
}

func (hc *HealthChecker) checkCustomResourceDefinitions(shouldExist bool) error {
	options := metav1.ListOptions{
		LabelSelector: k8s.ControllerNSLabel,
	}
	crdList, err := hc.kubeAPI.Apiextensions.ApiextensionsV1beta1().CustomResourceDefinitions().List(options)
	if err != nil {
		return err
	}

	objects := []runtime.Object{}
	for _, item := range crdList.Items {
		item := item // pin
		objects = append(objects, &item)
	}

	return checkResources("CustomResourceDefinitions", objects, []string{"serviceprofiles.linkerd.io"}, shouldExist)
}

func (hc *HealthChecker) checkMutatingWebhookConfigurations(shouldExist bool) error {
	options := metav1.ListOptions{
		LabelSelector: k8s.ControllerNSLabel,
	}
	mwc, err := hc.kubeAPI.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().List(options)
	if err != nil {
		return err
	}

	objects := []runtime.Object{}
	for _, item := range mwc.Items {
		item := item // pin
		objects = append(objects, &item)
	}

	return checkResources("MutatingWebhookConfigurations", objects, []string{k8s.ProxyInjectorWebhookConfigName}, shouldExist)
}

func (hc *HealthChecker) checkValidatingWebhookConfigurations(shouldExist bool) error {
	options := metav1.ListOptions{
		LabelSelector: k8s.ControllerNSLabel,
	}
	vwc, err := hc.kubeAPI.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().List(options)
	if err != nil {
		return err
	}

	objects := []runtime.Object{}
	for _, item := range vwc.Items {
		item := item // pin
		objects = append(objects, &item)
	}

	return checkResources("ValidatingWebhookConfigurations", objects, []string{k8s.SPValidatorWebhookConfigName}, shouldExist)
}

func (hc *HealthChecker) checkPodSecurityPolicies(shouldExist bool) error {
	options := metav1.ListOptions{
		LabelSelector: k8s.ControllerNSLabel,
	}
	psp, err := hc.kubeAPI.PolicyV1beta1().PodSecurityPolicies().List(options)
	if err != nil {
		return err
	}

	objects := []runtime.Object{}
	for _, item := range psp.Items {

		if hc.skipNoInitContainerResources(item.ObjectMeta.Labels) {
			continue
		}

		item := item // pin
		objects = append(objects, &item)
	}

	return checkResources("PodSecurityPolicies", objects, []string{fmt.Sprintf("linkerd-%s-control-plane", hc.ControlPlaneNamespace)}, shouldExist)
}

type meshedPodData struct {
	name       string
	namespace  string
	trustRoots []*x509.Certificate
}

func (hc *HealthChecker) getMeshedPods() ([]meshedPodData, error) {
	list, err := hc.kubeAPI.CoreV1().Pods(hc.DataPlaneNamespace).List(metav1.ListOptions{LabelSelector: k8s.ControllerNSLabel})
	if err != nil {
		return nil, err
	}
	var meshedPods []meshedPodData
	for _, p := range list.Items {
		for _, containerSpec := range p.Spec.Containers {
			if containerSpec.Name != k8s.ProxyContainerName {
				continue
			}

			for _, envVar := range containerSpec.Env {
				if envVar.Name != identity.EnvTrustAnchors {
					continue
				}

				encodedTrustAnchors := strings.TrimSpace(envVar.Value)
				roots, err := tls.DecodePEMCertificates(encodedTrustAnchors)
				if err != nil {
					meshedPods = append(meshedPods, meshedPodData{name: p.Name, namespace: p.Namespace, trustRoots: nil})
				} else {
					meshedPods = append(meshedPods, meshedPodData{name: p.Name, namespace: p.Namespace, trustRoots: roots})
				}
			}
		}
	}
	return meshedPods, nil
}

func checkResources(resourceName string, objects []runtime.Object, expectedNames []string, shouldExist bool) error {
	if !shouldExist {
		if len(objects) > 0 {
			resources := []Resource{}
			for _, obj := range objects {
				m, err := meta.Accessor(obj)
				if err != nil {
					return err
				}

				res := Resource{name: m.GetName()}
				gvks, _, err := k8s.ObjectKinds(obj)
				if err == nil && len(gvks) > 0 {
					res.groupVersionKind = gvks[0]
				}
				resources = append(resources, res)
			}
			return &ResourceError{resourceName, resources}
		}
		return nil
	}

	expected := map[string]bool{}
	for _, name := range expectedNames {
		expected[name] = false
	}

	for _, obj := range objects {
		metaObj, err := meta.Accessor(obj)
		if err != nil {
			return err
		}

		if _, ok := expected[metaObj.GetName()]; ok {
			expected[metaObj.GetName()] = true
		}
	}

	missing := []string{}
	for name, found := range expected {
		if !found {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("missing %s: %s", resourceName, strings.Join(missing, ", "))
	}

	return nil
}

func (hc *HealthChecker) getDataPlanePods(ctx context.Context) ([]*pb.Pod, error) {
	req := &pb.ListPodsRequest{}
	if hc.DataPlaneNamespace != "" {
		req.Selector = &pb.ResourceSelection{
			Resource: &pb.Resource{
				Namespace: hc.DataPlaneNamespace,
			},
		}
	}

	resp, err := hc.apiClient.ListPods(ctx, req)
	if err != nil {
		return nil, err
	}

	pods := make([]*pb.Pod, 0)
	for _, pod := range resp.GetPods() {
		if pod.ControllerNamespace == hc.ControlPlaneNamespace {
			pods = append(pods, pod)
		}
	}

	return pods, nil
}

func (hc *HealthChecker) checkCanPerformAction(verb, namespace, group, version, resource string) error {
	if hc.kubeAPI == nil {
		// we should never get here
		return fmt.Errorf("unexpected error: Kubernetes ClientSet not initialized")
	}

	return k8s.ResourceAuthz(
		hc.kubeAPI,
		namespace,
		verb,
		group,
		version,
		resource,
		"",
	)
}

func (hc *HealthChecker) checkCanCreate(namespace, group, version, resource string) error {
	return hc.checkCanPerformAction("create", namespace, group, version, resource)
}

func (hc *HealthChecker) checkCanGet(namespace, group, version, resource string) error {
	return hc.checkCanPerformAction("get", namespace, group, version, resource)
}

func (hc *HealthChecker) checkCapability(cap string) error {
	if hc.kubeAPI == nil {
		// we should never get here
		return fmt.Errorf("unexpected error: Kubernetes ClientSet not initialized")
	}

	pspList, err := hc.kubeAPI.PolicyV1beta1().PodSecurityPolicies().List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	if len(pspList.Items) == 0 {
		// no PodSecurityPolicies found, assume PodSecurityPolicy admission controller is disabled
		return nil
	}

	// if PodSecurityPolicies are found, validate one exists that:
	// 1) permits usage
	// AND
	// 2) provides the specified capability
	for _, psp := range pspList.Items {
		err := k8s.ResourceAuthz(
			hc.kubeAPI,
			"",
			"use",
			"policy",
			"v1beta1",
			"podsecuritypolicies",
			psp.GetName(),
		)
		if err == nil {
			for _, capability := range psp.Spec.AllowedCapabilities {
				if capability == "*" || string(capability) == cap {
					return nil
				}
			}
		}
	}

	return fmt.Errorf("found %d PodSecurityPolicies, but none provide %s, proxy injection will fail if the PSP admission controller is running", len(pspList.Items), cap)
}

func (hc *HealthChecker) checkClockSkew() error {
	if hc.kubeAPI == nil {
		// we should never get here
		return fmt.Errorf("unexpected error: Kubernetes ClientSet not initialized")
	}

	var clockSkewNodes []string

	nodeList, err := hc.kubeAPI.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, node := range nodeList.Items {
		for _, condition := range node.Status.Conditions {
			// we want to check only KubeletReady condition and only execute if the node is ready
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				since := time.Since(condition.LastHeartbeatTime.Time)
				if (since > AllowedClockSkew) || (since < -AllowedClockSkew) {
					clockSkewNodes = append(clockSkewNodes, node.Name)
				}
			}
		}
	}

	if len(clockSkewNodes) > 0 {
		return fmt.Errorf("clock skew detected for node(s): %s", strings.Join(clockSkewNodes, ", "))
	}

	return nil
}

// getPodStatuses returns a map of all Linkerd container statuses:
// component =>
//   name name =>
//     container statuses
// "controller" =>
//   "linkerd-controller-6f78cbd47-bc557" =>
//     [destination status, public-api status, ...]
func getPodStatuses(pods []corev1.Pod) map[string]map[string][]corev1.ContainerStatus {
	statuses := make(map[string]map[string][]corev1.ContainerStatus)

	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodRunning && strings.HasPrefix(pod.Name, "linkerd-") {
			parts := strings.Split(pod.Name, "-")
			// All control plane pods should have a name that results in at least 4
			// substrings when string.Split on '-'
			if len(parts) >= 4 {
				name := strings.Join(parts[1:len(parts)-2], "-")
				if _, found := statuses[name]; !found {
					statuses[name] = make(map[string][]corev1.ContainerStatus)
				}
				statuses[name][pod.Name] = pod.Status.ContainerStatuses
			}
		}
	}

	return statuses
}

const running = "Running"

func validateControlPlanePods(pods []corev1.Pod) error {
	statuses := getPodStatuses(pods)

	names := []string{"controller", "grafana", "identity", "prometheus", "sp-validator", "web", "tap"}
	// TODO: deprecate this when we drop support for checking pre-default proxy-injector control-planes
	if _, found := statuses["proxy-injector"]; found {
		names = append(names, "proxy-injector")
	}

	for _, name := range names {
		pods, found := statuses[name]
		if !found {
			return fmt.Errorf("No running pods for \"linkerd-%s\"", name)
		}
		var err error
		var ready bool
		for pod, containers := range pods {
			containersReady := true
			for _, container := range containers {
				if !container.Ready {
					// TODO: Save this as a Warning, allow check to pass but let the user
					// know there is at least one name not ready. This might imply
					// restructuring health checks to allow individual checks to return
					// either Fatal or Warning, rather than setting this property at
					// compile time.
					err = fmt.Errorf("name/%s container %s is not ready", pod, container.Name)
					containersReady = false
				}
			}
			if containersReady {
				// at least one name has all containers ready
				ready = true
				break
			}
		}
		if !ready {
			return err
		}
	}

	return nil
}

func checkControllerRunning(pods []corev1.Pod) error {
	statuses := getPodStatuses(pods)
	if _, ok := statuses["controller"]; !ok {
		for _, pod := range pods {
			podStatus := k8s.GetPodStatus(pod)
			if podStatus != running {
				return fmt.Errorf("%s status is %s", pod.Name, podStatus)
			}
		}
		return errors.New("No running pods for \"linkerd-controller\"")
	}
	return nil
}

func validateDataPlanePods(pods []*pb.Pod, targetNamespace string) error {
	if len(pods) == 0 {
		msg := fmt.Sprintf("No \"%s\" containers found", k8s.ProxyContainerName)
		if targetNamespace != "" {
			msg += fmt.Sprintf(" in the \"%s\" namespace", targetNamespace)
		}
		return fmt.Errorf(msg)
	}

	for _, pod := range pods {
		if pod.Status != "Running" {
			return fmt.Errorf("The \"%s\" name is not running",
				pod.Name)
		}

		if !pod.ProxyReady {
			return fmt.Errorf("The \"%s\" container in the \"%s\" name is not ready",
				k8s.ProxyContainerName, pod.Name)
		}
	}

	return nil
}

func validateDataPlanePodReporting(pods []*pb.Pod) error {
	notInPrometheus := []string{}

	for _, p := range pods {
		// the `Added` field indicates the name was found in Prometheus
		if !p.Added {
			notInPrometheus = append(notInPrometheus, p.Name)
		}
	}

	errMsg := ""
	if len(notInPrometheus) > 0 {
		errMsg = fmt.Sprintf("Data plane metrics not found for %s.", strings.Join(notInPrometheus, ", "))
	}

	if errMsg != "" {
		return fmt.Errorf(errMsg)
	}

	return nil
}

func checkUnschedulablePods(pods []corev1.Pod) error {
	var errors []string
	for _, pod := range pods {
		for _, condition := range pod.Status.Conditions {
			if condition.Reason == corev1.PodReasonUnschedulable {
				errors = append(errors, fmt.Sprintf("%s: %s", pod.Name, condition.Message))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("%s", strings.Join(errors, "\n    "))
	}

	return nil
}

func checkControlPlaneReplicaSets(rst []appsv1.ReplicaSet) error {
	var errors []string
	for _, rs := range rst {
		for _, r := range rs.Status.Conditions {
			if r.Type == appsv1.ReplicaSetReplicaFailure && r.Status == corev1.ConditionTrue {
				errors = append(errors, fmt.Sprintf("%s: %s", r.Reason, r.Message))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("%s", strings.Join(errors, "\n   "))
	}

	return nil
}

func (hc *HealthChecker) skipNoInitContainerResources(labelMap map[string]string) bool {

	if hc.Options.NoInitContainer {
		skip, err := strconv.ParseBool(labelMap[linkerdCniResourceLabel])
		if err != nil {
			log.Errorf("Error parsing %v, %v",
				linkerdCniResourceLabel, err)
		}
		return skip
	}

	return false
}
