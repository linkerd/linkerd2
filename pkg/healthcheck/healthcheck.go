package healthcheck

import (
	"bufio"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	controllerK8s "github.com/linkerd/linkerd2/controller/k8s"
	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/identity"
	"github.com/linkerd/linkerd2/pkg/issuercerts"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/pkg/util"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	admissionRegistration "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"
	k8sVersion "k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	apiregistrationv1client "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/typed/apiregistration/v1"
	"sigs.k8s.io/yaml"
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

	// LinkerdCRDChecks adds checks to validate that the control plane CRDs
	// exist. These checks can be run after installing the control plane CRDs
	// but before installing the control plane itself.
	LinkerdCRDChecks CategoryID = "linkerd-crd"

	// LinkerdConfigChecks enabled by `linkerd check config`

	// LinkerdConfigChecks adds a series of checks to validate that the Linkerd
	// namespace, RBAC, ServiceAccounts, and CRDs were successfully created.
	// These checks specifically validate that the `linkerd install config`
	// command succeeded in a multi-stage install, but also applies to a default
	// `linkerd install`.
	// These checks are dependent on the output of KubernetesAPIChecks, so those
	// checks must be added first.
	LinkerdConfigChecks CategoryID = "linkerd-config"

	// LinkerdIdentity Checks the integrity of the mTLS certificates
	// that the control plane is configured with
	LinkerdIdentity CategoryID = "linkerd-identity"

	// LinkerdWebhooksAndAPISvcTLS the integrity of the mTLS certificates
	// that of the for the injector and sp webhooks and the tap api svc
	LinkerdWebhooksAndAPISvcTLS CategoryID = "linkerd-webhooks-and-apisvc-tls"

	// LinkerdIdentityDataPlane checks that integrity of the mTLS
	// certificates that the proxies are configured with and tries to
	// report useful information with respect to whether the configuration
	// is compatible with the one of the control plane
	LinkerdIdentityDataPlane CategoryID = "linkerd-identity-data-plane"

	// LinkerdControlPlaneExistenceChecks adds a series of checks to validate that
	// the control plane namespace and controller pod exist.
	// These checks are dependent on the output of KubernetesAPIChecks, so those
	// checks must be added first.
	LinkerdControlPlaneExistenceChecks CategoryID = "linkerd-existence"

	// LinkerdVersionChecks adds a series of checks to query for the latest
	// version, and validate the CLI is up to date.
	LinkerdVersionChecks CategoryID = "linkerd-version"

	// LinkerdControlPlaneVersionChecks adds a series of checks to validate that
	// the control plane is running the latest available version.
	// These checks are dependent on the following:
	// 1) `latestVersions` from LinkerdVersionChecks
	// 2) `serverVersion` from `LinkerdControlPlaneExistenceChecks`
	LinkerdControlPlaneVersionChecks CategoryID = "control-plane-version"

	// LinkerdDataPlaneChecks adds data plane checks to validate that the
	// data plane namespace exists, and that the proxy containers are in a
	// ready state and running the latest available version.  These checks
	// are dependent on the output of KubernetesAPIChecks and
	// `latestVersions` from LinkerdVersionChecks, so those checks must be
	// added first.
	LinkerdDataPlaneChecks CategoryID = "linkerd-data-plane"

	// LinkerdControlPlaneProxyChecks adds data plane checks to validate the
	// control-plane proxies. The checkers include running and version checks
	LinkerdControlPlaneProxyChecks CategoryID = "linkerd-control-plane-proxy"

	// LinkerdHAChecks adds checks to validate that the HA configuration
	// is correct. These checks are no ops if linkerd is not in HA mode
	LinkerdHAChecks CategoryID = "linkerd-ha-checks"

	// LinkerdCNIPluginChecks adds checks to validate that the CNI
	/// plugin is installed and ready
	LinkerdCNIPluginChecks CategoryID = "linkerd-cni-plugin"

	// LinkerdOpaquePortsDefinitionChecks adds checks to validate that the
	// "opaque ports" annotation has been defined both in the service and the
	// corresponding pods
	LinkerdOpaquePortsDefinitionChecks CategoryID = "linkerd-opaque-ports-definition"

	// LinkerdCNIResourceLabel is the label key that is used to identify
	// whether a Kubernetes resource is related to the install-cni command
	// The value is expected to be "true", "false" or "", where "false" and
	// "" are equal, making "false" the default
	LinkerdCNIResourceLabel = "linkerd.io/cni-resource"

	linkerdCNIDisabledSkipReason = "skipping check because CNI is not enabled"
	linkerdCNIResourceName       = "linkerd-cni"
	linkerdCNIConfigMapName      = "linkerd-cni-config"

	podCIDRUnavailableSkipReason    = "skipping check because the nodes aren't exposing podCIDR"
	configMapDoesNotExistSkipReason = "skipping check because ConigMap does not exist"

	proxyInjectorOldTLSSecretName = "linkerd-proxy-injector-tls"
	proxyInjectorTLSSecretName    = "linkerd-proxy-injector-k8s-tls"

	spValidatorOldTLSSecretName = "linkerd-sp-validator-tls"
	spValidatorTLSSecretName    = "linkerd-sp-validator-k8s-tls"

	policyValidatorTLSSecretName = "linkerd-policy-validator-k8s-tls"
	certOldKeyName               = "crt.pem"
	certKeyName                  = "tls.crt"
	keyOldKeyName                = "key.pem"
	keyKeyName                   = "tls.key"
)

// AllowedClockSkew sets the allowed skew in clock synchronization
// between the system running inject command and the node(s), being
// based on assumed node's heartbeat interval (5 minutes) plus default TLS
// clock skew allowance.
//
// TODO: Make this default value overridable, e.g. by CLI flag
const AllowedClockSkew = 5*time.Minute + tls.DefaultClockSkewAllowance

var linkerdHAControlPlaneComponents = []string{
	"linkerd-destination",
	"linkerd-identity",
	"linkerd-proxy-injector",
}

// ExpectedServiceAccountNames is a list of the service accounts that a healthy
// Linkerd installation should have. Note that linkerd-heartbeat is optional,
// so it doesn't appear here.
var ExpectedServiceAccountNames = []string{
	"linkerd-destination",
	"linkerd-identity",
	"linkerd-proxy-injector",
}

var (
	retryWindow = 5 * time.Second
	// RequestTimeout is the time it takes for a request to timeout
	RequestTimeout = 30 * time.Second
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
func (e ResourceError) Error() string {
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
func (e CategoryError) Error() string {
	return e.Err.Error()
}

// IsCategoryError returns true if passed in error is of type CategoryError and belong to the given category
func IsCategoryError(err error, categoryID CategoryID) bool {
	var ce CategoryError
	if errors.As(err, &ce) {
		return ce.Category == categoryID
	}
	return false
}

// SkipError is returned by a check in case this check needs to be ignored.
type SkipError struct {
	Reason string
}

// Error satisfies the error interface for SkipError.
func (e SkipError) Error() string {
	return e.Reason
}

// VerboseSuccess implements the error interface but represents a success with
// a message.
type VerboseSuccess struct {
	Message string
}

// Error satisfies the error interface for VerboseSuccess.  Since VerboseSuccess
// does not actually represent a failure, this returns the empty string.
func (e VerboseSuccess) Error() string {
	return ""
}

// Checker is a smallest unit performing a single check
type Checker struct {
	// description is the short description that's printed to the command line
	// when the check is executed
	description string

	// hintAnchor, when appended to `HintBaseURL`, provides a URL to more
	// information about the check
	hintAnchor string

	// fatal indicates that all remaining checks should be aborted if this check
	// fails; it should only be used if subsequent checks cannot possibly succeed
	// (default false)
	fatal bool

	// warning indicates that if this check fails, it should be reported, but it
	// should not impact the overall outcome of the health check (default false)
	warning bool

	// retryDeadline establishes a deadline before which this check should be
	// retried; if the deadline has passed, the check fails (default: no retries)
	retryDeadline time.Time

	// surfaceErrorOnRetry indicates that the error message should be displayed
	// even if the check will be retried.  This is useful if the error message
	// contains the current status of the check.
	surfaceErrorOnRetry bool

	// check is the function that's called to execute the check; if the function
	// returns an error, the check fails
	check func(context.Context) error
}

// NewChecker returns a new instance of checker type
func NewChecker(description string) *Checker {
	return &Checker{
		description:   description,
		retryDeadline: time.Time{},
	}
}

// WithHintAnchor returns a checker with the given hint anchor
func (c *Checker) WithHintAnchor(hint string) *Checker {
	c.hintAnchor = hint
	return c
}

// Fatal returns a checker with the fatal field set
func (c *Checker) Fatal() *Checker {
	c.fatal = true
	return c
}

// Warning returns a checker with the warning field set
func (c *Checker) Warning() *Checker {
	c.warning = true
	return c
}

// WithRetryDeadline returns a checker with the provided retry timeout
func (c *Checker) WithRetryDeadline(retryDeadLine time.Time) *Checker {
	c.retryDeadline = retryDeadLine
	return c
}

// SurfaceErrorOnRetry returns a checker with the surfaceErrorOnRetry set
func (c *Checker) SurfaceErrorOnRetry() *Checker {
	c.surfaceErrorOnRetry = true
	return c
}

// WithCheck returns a checker with the provided check func
func (c *Checker) WithCheck(check func(context.Context) error) *Checker {
	c.check = check
	return c
}

// CheckResult encapsulates a check's identifying information and output
// Note there exists an analogous user-facing type, `cmd.check`, for output via
// `linkerd check -o json`.
type CheckResult struct {
	Category    CategoryID
	Description string
	HintURL     string
	Retry       bool
	Warning     bool
	Err         error
}

// CheckObserver receives the results of each check.
type CheckObserver func(*CheckResult)

// Category is a group of checkers, to check a particular component or use-case
type Category struct {
	ID       CategoryID
	checkers []Checker
	enabled  bool
	// hintBaseURL provides a base URL with more information
	// about the check
	hintBaseURL string
}

// NewCategory returns an instance of Category with the specified data
func NewCategory(id CategoryID, checkers []Checker, enabled bool) *Category {
	return &Category{
		ID:          id,
		checkers:    checkers,
		enabled:     enabled,
		hintBaseURL: HintBaseURL(version.Version),
	}
}

// WithHintBaseURL returns a Category with the provided hintBaseURL
func (c *Category) WithHintBaseURL(hintBaseURL string) *Category {
	c.hintBaseURL = hintBaseURL
	return c
}

// Options specifies configuration for a HealthChecker.
type Options struct {
	IsMainCheckCommand    bool
	ControlPlaneNamespace string
	CNINamespace          string
	DataPlaneNamespace    string
	KubeConfig            string
	KubeContext           string
	Impersonate           string
	ImpersonateGroup      []string
	APIAddr               string
	VersionOverride       string
	RetryDeadline         time.Time
	CNIEnabled            bool
	InstallManifest       string
	CRDManifest           string
	ChartValues           *l5dcharts.Values
}

// HealthChecker encapsulates all health check checkers, and clients required to
// perform those checks.
type HealthChecker struct {
	categories []*Category
	*Options

	// these fields are set in the process of running checks
	kubeAPI          *k8s.KubernetesAPI
	kubeVersion      *k8sVersion.Info
	controlPlanePods []corev1.Pod
	LatestVersions   version.Channels
	serverVersion    string
	linkerdConfig    *l5dcharts.Values
	uuid             string
	issuerCert       *tls.Cred
	trustAnchors     []*x509.Certificate
	cniDaemonSet     *appsv1.DaemonSet
}

// Runner is implemented by any health-checkers that can be triggered with RunChecks()
type Runner interface {
	RunChecks(observer CheckObserver) (bool, bool)
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
		if _, ok := checkMap[hc.categories[i].ID]; ok {
			hc.categories[i].enabled = true
		}
	}

	return hc
}

// InitializeKubeAPIClient creates a client for the HealthChecker. It avoids
// having to require the KubernetesAPIChecks check to run in order for the
// HealthChecker to run other checks.
func (hc *HealthChecker) InitializeKubeAPIClient() error {
	k8sAPI, err := k8s.NewAPI(hc.KubeConfig, hc.KubeContext, hc.Impersonate, hc.ImpersonateGroup, RequestTimeout)
	if err != nil {
		return err
	}
	hc.kubeAPI = k8sAPI

	return nil
}

// InitializeLinkerdGlobalConfig populates the linkerd config object in the
// healthchecker. It avoids having to require the LinkerdControlPlaneExistenceChecks
// check to run before running other checks
func (hc *HealthChecker) InitializeLinkerdGlobalConfig(ctx context.Context) error {
	uuid, l5dConfig, err := hc.checkLinkerdConfigConfigMap(ctx)
	if err != nil {
		return err
	}

	if l5dConfig != nil {
		hc.CNIEnabled = l5dConfig.CNIEnabled
	}
	hc.uuid = uuid
	hc.linkerdConfig = l5dConfig

	return nil
}

// AppendCategories returns a HealthChecker instance appending the provided Categories
func (hc *HealthChecker) AppendCategories(categories ...*Category) *HealthChecker {
	hc.categories = append(hc.categories, categories...)
	return hc
}

// GetCategories returns all the categories
func (hc *HealthChecker) GetCategories() []*Category {
	return hc.categories
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
// https://linkerd.io/{major-version}/checks/#
func (hc *HealthChecker) allCategories() []*Category {
	return []*Category{
		NewCategory(
			KubernetesAPIChecks,
			[]Checker{
				{
					description: "can initialize the client",
					hintAnchor:  "k8s-api",
					fatal:       true,
					check: func(context.Context) (err error) {
						err = hc.InitializeKubeAPIClient()
						return
					},
				},
				{
					description: "can query the Kubernetes API",
					hintAnchor:  "k8s-api",
					fatal:       true,
					check: func(ctx context.Context) (err error) {
						hc.kubeVersion, err = hc.kubeAPI.GetVersionInfo()
						return
					},
				},
			},
			false,
		),
		NewCategory(
			KubernetesVersionChecks,
			[]Checker{
				{
					description: "is running the minimum Kubernetes API version",
					hintAnchor:  "k8s-version",
					check: func(context.Context) error {
						return hc.kubeAPI.CheckVersion(hc.kubeVersion)
					},
				},
			},
			false,
		),
		NewCategory(
			LinkerdPreInstallChecks,
			[]Checker{
				{
					description: "control plane namespace does not already exist",
					hintAnchor:  "pre-ns",
					check: func(ctx context.Context) error {
						return hc.CheckNamespace(ctx, hc.ControlPlaneNamespace, false)
					},
				},
				{
					description: "can create non-namespaced resources",
					hintAnchor:  "pre-k8s-cluster-k8s",
					check: func(ctx context.Context) error {
						return hc.checkCanCreateNonNamespacedResources(ctx)
					},
				},
				{
					description: "can create ServiceAccounts",
					hintAnchor:  "pre-k8s",
					check: func(ctx context.Context) error {
						return hc.checkCanCreate(ctx, hc.ControlPlaneNamespace, "", "v1", "serviceaccounts")
					},
				},
				{
					description: "can create Services",
					hintAnchor:  "pre-k8s",
					check: func(ctx context.Context) error {
						return hc.checkCanCreate(ctx, hc.ControlPlaneNamespace, "", "v1", "services")
					},
				},
				{
					description: "can create Deployments",
					hintAnchor:  "pre-k8s",
					check: func(ctx context.Context) error {
						return hc.checkCanCreate(ctx, hc.ControlPlaneNamespace, "apps", "v1", "deployments")
					},
				},
				{
					description: "can create CronJobs",
					hintAnchor:  "pre-k8s",
					check: func(ctx context.Context) error {
						return hc.checkCanCreate(ctx, hc.ControlPlaneNamespace, "batch", "v1beta1", "cronjobs")
					},
				},
				{
					description: "can create ConfigMaps",
					hintAnchor:  "pre-k8s",
					check: func(ctx context.Context) error {
						return hc.checkCanCreate(ctx, hc.ControlPlaneNamespace, "", "v1", "configmaps")
					},
				},
				{
					description: "can create Secrets",
					hintAnchor:  "pre-k8s",
					check: func(ctx context.Context) error {
						return hc.checkCanCreate(ctx, hc.ControlPlaneNamespace, "", "v1", "secrets")
					},
				},
				{
					description: "can read Secrets",
					hintAnchor:  "pre-k8s",
					check: func(ctx context.Context) error {
						return hc.checkCanGet(ctx, hc.ControlPlaneNamespace, "", "v1", "secrets")
					},
				},
				{
					description: "can read extension-apiserver-authentication configmap",
					hintAnchor:  "pre-k8s",
					check: func(ctx context.Context) error {
						return hc.checkExtensionAPIServerAuthentication(ctx)
					},
				},
				{
					description: "no clock skew detected",
					hintAnchor:  "pre-k8s-clock-skew",
					warning:     true,
					check: func(ctx context.Context) error {
						return hc.checkClockSkew(ctx)
					},
				},
			},
			false,
		),
		NewCategory(
			LinkerdCRDChecks,
			[]Checker{
				{
					description:   "control plane CustomResourceDefinitions exist",
					hintAnchor:    "l5d-existence-crd",
					fatal:         true,
					retryDeadline: hc.RetryDeadline,
					check: func(ctx context.Context) error {
						return CheckCustomResourceDefinitions(ctx, hc.kubeAPI, hc.CRDManifest)
					},
				},
			},
			false,
		),
		NewCategory(
			LinkerdControlPlaneExistenceChecks,
			[]Checker{
				{
					description: "'linkerd-config' config map exists",
					hintAnchor:  "l5d-existence-linkerd-config",
					fatal:       true,
					check: func(ctx context.Context) (err error) {
						err = hc.InitializeLinkerdGlobalConfig(ctx)
						return
					},
				},
				{
					description: "heartbeat ServiceAccount exist",
					hintAnchor:  "l5d-existence-sa",
					fatal:       true,
					check: func(ctx context.Context) error {
						if hc.isHeartbeatDisabled() {
							return nil
						}
						return hc.checkServiceAccounts(ctx, []string{"linkerd-heartbeat"}, hc.ControlPlaneNamespace, controlPlaneComponentsSelector())
					},
				},
				{
					description:   "control plane replica sets are ready",
					hintAnchor:    "l5d-existence-replicasets",
					retryDeadline: hc.RetryDeadline,
					fatal:         true,
					check: func(ctx context.Context) error {
						controlPlaneReplicaSet, err := hc.kubeAPI.GetReplicaSets(ctx, hc.ControlPlaneNamespace)
						if err != nil {
							return err
						}
						return checkControlPlaneReplicaSets(controlPlaneReplicaSet)
					},
				},
				{
					description:         "no unschedulable pods",
					hintAnchor:          "l5d-existence-unschedulable-pods",
					retryDeadline:       hc.RetryDeadline,
					surfaceErrorOnRetry: true,
					warning:             true,
					check: func(ctx context.Context) error {
						// do not save this into hc.controlPlanePods, as this check may
						// succeed prior to all expected control plane pods being up
						controlPlanePods, err := hc.kubeAPI.GetPodsByNamespace(ctx, hc.ControlPlaneNamespace)
						if err != nil {
							return err
						}
						return checkUnschedulablePods(controlPlanePods)
					},
				},
				{
					description:         "control plane pods are ready",
					hintAnchor:          "l5d-api-control-ready",
					retryDeadline:       hc.RetryDeadline,
					surfaceErrorOnRetry: true,
					fatal:               true,
					check: func(ctx context.Context) error {
						var err error
						podList, err := hc.kubeAPI.CoreV1().Pods(hc.ControlPlaneNamespace).List(ctx, metav1.ListOptions{
							LabelSelector: k8s.ControllerComponentLabel,
						})
						if err != nil {
							return err
						}
						hc.controlPlanePods = podList.Items
						return validateControlPlanePods(hc.controlPlanePods)
					},
				},
				{
					description: "cluster networks contains all node podCIDRs",
					hintAnchor:  "l5d-cluster-networks-cidr",
					check: func(ctx context.Context) error {
						// We explicitly initialize the config here so that we dont rely on the "l5d-existence-linkerd-config"
						// check to set the clusterNetworks value, since `linkerd check config` will skip that check.
						err := hc.InitializeLinkerdGlobalConfig(ctx)
						if err != nil {
							return err
						}
						return hc.checkClusterNetworks(ctx)
					},
				},
				{
					description: "cluster networks contains all pods",
					hintAnchor:  "l5d-cluster-networks-pods",
					check: func(ctx context.Context) error {
						return hc.checkClusterNetworksContainAllPods(ctx)
					},
				},
				{
					description: "cluster networks contains all services",
					hintAnchor:  "l5d-cluster-networks-pods",
					check: func(ctx context.Context) error {
						return hc.checkClusterNetworksContainAllServices(ctx)
					},
				},
			},
			false,
		),
		NewCategory(
			LinkerdConfigChecks,
			[]Checker{
				{
					description: "control plane Namespace exists",
					hintAnchor:  "l5d-existence-ns",
					fatal:       true,
					check: func(ctx context.Context) error {
						return hc.CheckNamespace(ctx, hc.ControlPlaneNamespace, true)
					},
				},
				{
					description: "control plane ClusterRoles exist",
					hintAnchor:  "l5d-existence-cr",
					fatal:       true,
					check: func(ctx context.Context) error {
						return hc.checkClusterRoles(ctx, true, hc.expectedRBACNames(), controlPlaneComponentsSelector())
					},
				},
				{
					description: "control plane ClusterRoleBindings exist",
					hintAnchor:  "l5d-existence-crb",
					fatal:       true,
					check: func(ctx context.Context) error {
						return hc.checkClusterRoleBindings(ctx, true, hc.expectedRBACNames(), controlPlaneComponentsSelector())
					},
				},
				{
					description: "control plane ServiceAccounts exist",
					hintAnchor:  "l5d-existence-sa",
					fatal:       true,
					check: func(ctx context.Context) error {
						return hc.checkServiceAccounts(ctx, ExpectedServiceAccountNames, hc.ControlPlaneNamespace, controlPlaneComponentsSelector())
					},
				},
				{
					description: "control plane CustomResourceDefinitions exist",
					hintAnchor:  "l5d-existence-crd",
					fatal:       true,
					check: func(ctx context.Context) error {
						return CheckCustomResourceDefinitions(ctx, hc.kubeAPI, hc.CRDManifest)
					},
				},
				{
					description: "control plane MutatingWebhookConfigurations exist",
					hintAnchor:  "l5d-existence-mwc",
					fatal:       true,
					check: func(ctx context.Context) error {
						return hc.checkMutatingWebhookConfigurations(ctx, true)
					},
				},
				{
					description: "control plane ValidatingWebhookConfigurations exist",
					hintAnchor:  "l5d-existence-vwc",
					fatal:       true,
					check: func(ctx context.Context) error {
						return hc.checkValidatingWebhookConfigurations(ctx, true)
					},
				},
				{
					description: "proxy-init container runs as root user if docker container runtime is used",
					hintAnchor:  "l5d-proxy-init-run-as-root",
					fatal:       false,
					check: func(ctx context.Context) error {
						// We explicitly initialize the config here so that we dont rely on the "l5d-existence-linkerd-config"
						// check to set the clusterNetworks value, since `linkerd check config` will skip that check.
						err := hc.InitializeLinkerdGlobalConfig(ctx)
						if err != nil {
							if kerrors.IsNotFound(err) {
								return SkipError{Reason: configMapDoesNotExistSkipReason}
							}
							return err
						}
						config := hc.LinkerdConfig()
						runAsRoot := config != nil && config.ProxyInit != nil && config.ProxyInit.RunAsRoot
						if !runAsRoot {
							return CheckNodesHaveNonDockerRuntime(ctx, hc.KubeAPIClient())
						}
						return nil
					},
				},
			},
			false,
		),
		NewCategory(
			LinkerdCNIPluginChecks,
			[]Checker{
				{
					description: "cni plugin ConfigMap exists",
					hintAnchor:  "cni-plugin-cm-exists",
					fatal:       true,
					check: func(ctx context.Context) error {
						if !hc.CNIEnabled {
							return SkipError{Reason: linkerdCNIDisabledSkipReason}
						}
						_, err := hc.kubeAPI.CoreV1().ConfigMaps(hc.CNINamespace).Get(ctx, linkerdCNIConfigMapName, metav1.GetOptions{})
						return err
					},
				},
				{
					description: "cni plugin ClusterRole exists",
					hintAnchor:  "cni-plugin-cr-exists",
					fatal:       true,
					check: func(ctx context.Context) error {
						if !hc.CNIEnabled {
							return SkipError{Reason: linkerdCNIDisabledSkipReason}
						}
						_, err := hc.kubeAPI.RbacV1().ClusterRoles().Get(ctx, linkerdCNIResourceName, metav1.GetOptions{})
						if kerrors.IsNotFound(err) {
							return fmt.Errorf("missing ClusterRole: %s", linkerdCNIResourceName)
						}
						return err
					},
				},
				{
					description: "cni plugin ClusterRoleBinding exists",
					hintAnchor:  "cni-plugin-crb-exists",
					fatal:       true,
					check: func(ctx context.Context) error {
						if !hc.CNIEnabled {
							return SkipError{Reason: linkerdCNIDisabledSkipReason}
						}
						_, err := hc.kubeAPI.RbacV1().ClusterRoleBindings().Get(ctx, linkerdCNIResourceName, metav1.GetOptions{})
						if kerrors.IsNotFound(err) {
							return fmt.Errorf("missing ClusterRoleBinding: %s", linkerdCNIResourceName)
						}
						return err
					},
				},
				{
					description: "cni plugin ServiceAccount exists",
					hintAnchor:  "cni-plugin-sa-exists",
					fatal:       true,
					check: func(ctx context.Context) error {
						if !hc.CNIEnabled {
							return SkipError{Reason: linkerdCNIDisabledSkipReason}
						}
						_, err := hc.kubeAPI.CoreV1().ServiceAccounts(hc.CNINamespace).Get(ctx, linkerdCNIResourceName, metav1.GetOptions{})
						if kerrors.IsNotFound(err) {
							return fmt.Errorf("missing ServiceAccount: %s", linkerdCNIResourceName)
						}
						return err
					},
				},
				{
					description: "cni plugin DaemonSet exists",
					hintAnchor:  "cni-plugin-ds-exists",
					fatal:       true,
					check: func(ctx context.Context) (err error) {
						if !hc.CNIEnabled {
							return SkipError{Reason: linkerdCNIDisabledSkipReason}
						}
						hc.cniDaemonSet, err = hc.kubeAPI.Interface.AppsV1().DaemonSets(hc.CNINamespace).Get(ctx, linkerdCNIResourceName, metav1.GetOptions{})
						if kerrors.IsNotFound(err) {
							return fmt.Errorf("missing DaemonSet: %s", linkerdCNIResourceName)
						}
						return err
					},
				},
				{
					description:         "cni plugin pod is running on all nodes",
					hintAnchor:          "cni-plugin-ready",
					retryDeadline:       hc.RetryDeadline,
					surfaceErrorOnRetry: true,
					fatal:               true,
					check: func(ctx context.Context) (err error) {
						if !hc.CNIEnabled {
							return SkipError{Reason: linkerdCNIDisabledSkipReason}
						}
						hc.cniDaemonSet, err = hc.kubeAPI.Interface.AppsV1().DaemonSets(hc.CNINamespace).Get(ctx, linkerdCNIResourceName, metav1.GetOptions{})
						if kerrors.IsNotFound(err) {
							return fmt.Errorf("missing DaemonSet: %s", linkerdCNIResourceName)
						}
						scheduled := hc.cniDaemonSet.Status.DesiredNumberScheduled
						ready := hc.cniDaemonSet.Status.NumberReady
						if scheduled != ready {
							return fmt.Errorf("number ready: %d, number scheduled: %d", ready, scheduled)
						}
						return nil
					},
				},
			},
			false,
		),
		NewCategory(
			LinkerdIdentity,
			[]Checker{
				{
					description: "certificate config is valid",
					hintAnchor:  "l5d-identity-cert-config-valid",
					fatal:       true,
					check: func(ctx context.Context) (err error) {
						hc.issuerCert, hc.trustAnchors, err = hc.checkCertificatesConfig(ctx)
						return
					},
				},
				{
					description: "trust anchors are using supported crypto algorithm",
					hintAnchor:  "l5d-identity-trustAnchors-use-supported-crypto",
					fatal:       true,
					check: func(context.Context) error {
						var invalidAnchors []string
						for _, anchor := range hc.trustAnchors {
							if err := issuercerts.CheckTrustAnchorAlgoRequirements(anchor); err != nil {
								invalidAnchors = append(invalidAnchors, fmt.Sprintf("* %v %s %s", anchor.SerialNumber, anchor.Subject.CommonName, err))
							}
						}
						if len(invalidAnchors) > 0 {
							return fmt.Errorf("Invalid trustAnchors:\n\t%s", strings.Join(invalidAnchors, "\n\t"))
						}
						return nil
					},
				},
				{
					description: "trust anchors are within their validity period",
					hintAnchor:  "l5d-identity-trustAnchors-are-time-valid",
					fatal:       true,
					check: func(ctx context.Context) error {
						var expiredAnchors []string
						for _, anchor := range hc.trustAnchors {
							if err := issuercerts.CheckCertValidityPeriod(anchor); err != nil {
								expiredAnchors = append(expiredAnchors, fmt.Sprintf("* %v %s %s", anchor.SerialNumber, anchor.Subject.CommonName, err))
							}
						}
						if len(expiredAnchors) > 0 {
							return fmt.Errorf("Invalid anchors:\n\t%s", strings.Join(expiredAnchors, "\n\t"))
						}

						return nil
					},
				},
				{
					description: "trust anchors are valid for at least 60 days",
					hintAnchor:  "l5d-identity-trustAnchors-not-expiring-soon",
					warning:     true,
					check: func(ctx context.Context) error {
						var expiringAnchors []string
						for _, anchor := range hc.trustAnchors {
							if err := issuercerts.CheckExpiringSoon(anchor); err != nil {
								expiringAnchors = append(expiringAnchors, fmt.Sprintf("* %v %s %s", anchor.SerialNumber, anchor.Subject.CommonName, err))
							}
						}
						if len(expiringAnchors) > 0 {
							return fmt.Errorf("Anchors expiring soon:\n\t%s", strings.Join(expiringAnchors, "\n\t"))
						}
						return nil
					},
				},
				{
					description: "issuer cert is using supported crypto algorithm",
					hintAnchor:  "l5d-identity-issuer-cert-uses-supported-crypto",
					fatal:       true,
					check: func(context.Context) error {
						if err := issuercerts.CheckIssuerCertAlgoRequirements(hc.issuerCert.Certificate); err != nil {
							return fmt.Errorf("issuer certificate %w", err)
						}
						return nil
					},
				},
				{
					description: "issuer cert is within its validity period",
					hintAnchor:  "l5d-identity-issuer-cert-is-time-valid",
					fatal:       true,
					check: func(ctx context.Context) error {
						if err := issuercerts.CheckCertValidityPeriod(hc.issuerCert.Certificate); err != nil {
							return fmt.Errorf("issuer certificate is %w", err)
						}
						return nil
					},
				},
				{
					description: "issuer cert is valid for at least 60 days",
					warning:     true,
					hintAnchor:  "l5d-identity-issuer-cert-not-expiring-soon",
					check: func(context.Context) error {
						if err := issuercerts.CheckExpiringSoon(hc.issuerCert.Certificate); err != nil {
							return fmt.Errorf("issuer certificate %w", err)
						}
						return nil
					},
				},
				{
					description: "issuer cert is issued by the trust anchor",
					hintAnchor:  "l5d-identity-issuer-cert-issued-by-trust-anchor",
					check: func(ctx context.Context) error {
						return hc.issuerCert.Verify(tls.CertificatesToPool(hc.trustAnchors), "", time.Time{})
					},
				},
			},
			false,
		),
		NewCategory(
			LinkerdWebhooksAndAPISvcTLS,
			[]Checker{
				{
					description: "proxy-injector webhook has valid cert",
					hintAnchor:  "l5d-proxy-injector-webhook-cert-valid",
					fatal:       true,
					check: func(ctx context.Context) (err error) {
						anchors, err := hc.fetchProxyInjectorCaBundle(ctx)
						if err != nil {
							return err
						}
						cert, err := hc.FetchCredsFromSecret(ctx, hc.ControlPlaneNamespace, proxyInjectorTLSSecretName)
						if kerrors.IsNotFound(err) {
							cert, err = hc.FetchCredsFromOldSecret(ctx, hc.ControlPlaneNamespace, proxyInjectorOldTLSSecretName)
						}
						if err != nil {
							return err
						}

						identityName := fmt.Sprintf("linkerd-proxy-injector.%s.svc", hc.ControlPlaneNamespace)
						return hc.CheckCertAndAnchors(cert, anchors, identityName)
					},
				},
				{
					description: "proxy-injector cert is valid for at least 60 days",
					warning:     true,
					hintAnchor:  "l5d-proxy-injector-webhook-cert-not-expiring-soon",
					check: func(ctx context.Context) error {
						cert, err := hc.FetchCredsFromSecret(ctx, hc.ControlPlaneNamespace, proxyInjectorTLSSecretName)
						if kerrors.IsNotFound(err) {
							cert, err = hc.FetchCredsFromOldSecret(ctx, hc.ControlPlaneNamespace, proxyInjectorOldTLSSecretName)
						}
						if err != nil {
							return err
						}
						return hc.CheckCertAndAnchorsExpiringSoon(cert)

					},
				},
				{
					description: "sp-validator webhook has valid cert",
					hintAnchor:  "l5d-sp-validator-webhook-cert-valid",
					fatal:       true,
					check: func(ctx context.Context) (err error) {
						anchors, err := hc.fetchWebhookCaBundle(ctx, k8s.SPValidatorWebhookConfigName)
						if err != nil {
							return err
						}
						cert, err := hc.FetchCredsFromSecret(ctx, hc.ControlPlaneNamespace, spValidatorTLSSecretName)
						if kerrors.IsNotFound(err) {
							cert, err = hc.FetchCredsFromOldSecret(ctx, hc.ControlPlaneNamespace, spValidatorOldTLSSecretName)
						}
						if err != nil {
							return err
						}
						identityName := fmt.Sprintf("linkerd-sp-validator.%s.svc", hc.ControlPlaneNamespace)
						return hc.CheckCertAndAnchors(cert, anchors, identityName)
					},
				},
				{
					description: "sp-validator cert is valid for at least 60 days",
					warning:     true,
					hintAnchor:  "l5d-sp-validator-webhook-cert-not-expiring-soon",
					check: func(ctx context.Context) error {
						cert, err := hc.FetchCredsFromSecret(ctx, hc.ControlPlaneNamespace, spValidatorTLSSecretName)
						if kerrors.IsNotFound(err) {
							cert, err = hc.FetchCredsFromOldSecret(ctx, hc.ControlPlaneNamespace, spValidatorOldTLSSecretName)
						}
						if err != nil {
							return err
						}
						return hc.CheckCertAndAnchorsExpiringSoon(cert)

					},
				},
				{
					description: "policy-validator webhook has valid cert",
					hintAnchor:  "l5d-policy-validator-webhook-cert-valid",
					fatal:       true,
					check: func(ctx context.Context) (err error) {
						anchors, err := hc.fetchWebhookCaBundle(ctx, k8s.PolicyValidatorWebhookConfigName)
						if kerrors.IsNotFound(err) {
							return SkipError{Reason: "policy-validator not installed"}
						}
						if err != nil {
							return err
						}
						cert, err := hc.FetchCredsFromSecret(ctx, hc.ControlPlaneNamespace, policyValidatorTLSSecretName)
						if kerrors.IsNotFound(err) {
							return SkipError{Reason: "policy-validator not installed"}
						}
						if err != nil {
							return err
						}
						identityName := fmt.Sprintf("linkerd-policy-validator.%s.svc", hc.ControlPlaneNamespace)
						return hc.CheckCertAndAnchors(cert, anchors, identityName)
					},
				},
				{
					description: "policy-validator cert is valid for at least 60 days",
					warning:     true,
					hintAnchor:  "l5d-policy-validator-webhook-cert-not-expiring-soon",
					check: func(ctx context.Context) error {
						cert, err := hc.FetchCredsFromSecret(ctx, hc.ControlPlaneNamespace, policyValidatorTLSSecretName)
						if kerrors.IsNotFound(err) {
							return SkipError{Reason: "policy-validator not installed"}
						}
						if err != nil {
							return err
						}
						return hc.CheckCertAndAnchorsExpiringSoon(cert)

					},
				},
			},
			false,
		),
		NewCategory(
			LinkerdIdentityDataPlane,
			[]Checker{
				{
					description: "data plane proxies certificate match CA",
					hintAnchor:  "l5d-identity-data-plane-proxies-certs-match-ca",
					warning:     true,
					check: func(ctx context.Context) error {
						return hc.checkDataPlaneProxiesCertificate(ctx)
					},
				},
			},
			false,
		),
		NewCategory(
			LinkerdVersionChecks,
			[]Checker{
				{
					description: "can determine the latest version",
					hintAnchor:  "l5d-version-latest",
					warning:     true,
					check: func(ctx context.Context) (err error) {
						if hc.VersionOverride != "" {
							hc.LatestVersions, err = version.NewChannels(hc.VersionOverride)
						} else {
							uuid := "unknown"
							if hc.uuid != "" {
								uuid = hc.uuid
							}
							hc.LatestVersions, err = version.GetLatestVersions(ctx, uuid, "cli")
						}
						return
					},
				},
				{
					description: "cli is up-to-date",
					hintAnchor:  "l5d-version-cli",
					warning:     true,
					check: func(context.Context) error {
						return hc.LatestVersions.Match(version.Version)
					},
				},
			},
			false,
		),
		NewCategory(
			LinkerdControlPlaneVersionChecks,
			[]Checker{
				{
					description:   "can retrieve the control plane version",
					hintAnchor:    "l5d-version-control",
					retryDeadline: hc.RetryDeadline,
					fatal:         true,
					check: func(ctx context.Context) (err error) {
						hc.serverVersion, err = GetServerVersion(ctx, hc.ControlPlaneNamespace, hc.kubeAPI)
						return
					},
				},
				{
					description: "control plane is up-to-date",
					hintAnchor:  "l5d-version-control",
					warning:     true,
					check: func(context.Context) error {
						return hc.LatestVersions.Match(hc.serverVersion)
					},
				},
				{
					description: "control plane and cli versions match",
					hintAnchor:  "l5d-version-control",
					warning:     true,
					check: func(context.Context) error {
						if hc.serverVersion != version.Version {
							return fmt.Errorf("control plane running %s but cli running %s", hc.serverVersion, version.Version)
						}
						return nil
					},
				},
			},
			false,
		),
		NewCategory(
			LinkerdControlPlaneProxyChecks,
			[]Checker{
				{
					description:         "control plane proxies are healthy",
					hintAnchor:          "l5d-cp-proxy-healthy",
					retryDeadline:       hc.RetryDeadline,
					surfaceErrorOnRetry: true,
					fatal:               true,
					check: func(ctx context.Context) error {
						return hc.CheckProxyHealth(ctx, hc.ControlPlaneNamespace, hc.ControlPlaneNamespace)
					},
				},
				{
					description: "control plane proxies are up-to-date",
					hintAnchor:  "l5d-cp-proxy-version",
					warning:     true,
					check: func(ctx context.Context) error {
						podList, err := hc.kubeAPI.CoreV1().Pods(hc.ControlPlaneNamespace).List(ctx, metav1.ListOptions{LabelSelector: k8s.ControllerNSLabel})
						if err != nil {
							return err
						}

						return hc.CheckProxyVersionsUpToDate(podList.Items)
					},
				},
				{
					description: "control plane proxies and cli versions match",
					hintAnchor:  "l5d-cp-proxy-cli-version",
					warning:     true,
					check: func(ctx context.Context) error {
						podList, err := hc.kubeAPI.CoreV1().Pods(hc.ControlPlaneNamespace).List(ctx, metav1.ListOptions{LabelSelector: k8s.ControllerNSLabel})
						if err != nil {
							return err
						}

						return CheckIfProxyVersionsMatchWithCLI(podList.Items)
					},
				},
			},
			false,
		),
		NewCategory(
			LinkerdDataPlaneChecks,
			[]Checker{
				{
					description: "data plane namespace exists",
					hintAnchor:  "l5d-data-plane-exists",
					fatal:       true,
					check: func(ctx context.Context) error {
						if hc.DataPlaneNamespace == "" {
							// when checking proxies in all namespaces, this check is a no-op
							return nil
						}
						return hc.CheckNamespace(ctx, hc.DataPlaneNamespace, true)
					},
				},
				{
					description:   "data plane proxies are ready",
					hintAnchor:    "l5d-data-plane-ready",
					retryDeadline: hc.RetryDeadline,
					fatal:         true,
					check: func(ctx context.Context) error {
						pods, err := hc.GetDataPlanePods(ctx)
						if err != nil {
							return err
						}
						return CheckPodsRunning(pods, hc.DataPlaneNamespace)
					},
				},
				{
					description: "data plane is up-to-date",
					hintAnchor:  "l5d-data-plane-version",
					warning:     true,
					check: func(ctx context.Context) error {
						pods, err := hc.GetDataPlanePods(ctx)
						if err != nil {
							return err
						}

						return hc.CheckProxyVersionsUpToDate(pods)
					},
				},
				{
					description: "data plane and cli versions match",
					hintAnchor:  "l5d-data-plane-cli-version",
					warning:     true,
					check: func(ctx context.Context) error {
						pods, err := hc.GetDataPlanePods(ctx)
						if err != nil {
							return err
						}

						return CheckIfProxyVersionsMatchWithCLI(pods)
					},
				},
				{
					description: "data plane pod labels are configured correctly",
					hintAnchor:  "l5d-data-plane-pod-labels",
					warning:     true,
					check: func(ctx context.Context) error {
						pods, err := hc.GetDataPlanePods(ctx)
						if err != nil {
							return err
						}

						return checkMisconfiguredPodsLabels(pods)
					},
				},
				{
					description: "data plane service labels are configured correctly",
					hintAnchor:  "l5d-data-plane-services-labels",
					warning:     true,
					check: func(ctx context.Context) error {
						services, err := hc.GetServices(ctx)
						if err != nil {
							return err
						}

						return checkMisconfiguredServiceLabels(services)
					},
				},
				{
					description: "data plane service annotations are configured correctly",
					hintAnchor:  "l5d-data-plane-services-annotations",
					warning:     true,
					check: func(ctx context.Context) error {
						services, err := hc.GetServices(ctx)
						if err != nil {
							return err
						}

						return checkMisconfiguredServiceAnnotations(services)
					},
				},
				{
					description: "opaque ports are properly annotated",
					hintAnchor:  "linkerd-opaque-ports-definition",
					warning:     true,
					check: func(ctx context.Context) error {
						return hc.checkMisconfiguredOpaquePortAnnotations(ctx)
					},
				},
			},
			false,
		),
		NewCategory(
			LinkerdHAChecks,
			[]Checker{
				{
					description: "pod injection disabled on kube-system",
					hintAnchor:  "l5d-injection-disabled",
					warning:     true,
					check: func(ctx context.Context) error {
						policy, err := hc.getMutatingWebhookFailurePolicy(ctx)
						if err != nil {
							return err
						}
						if policy != nil && *policy == admissionRegistration.Fail {
							return hc.checkHAMetadataPresentOnKubeSystemNamespace(ctx)
						}
						return SkipError{Reason: "not run for non HA installs"}
					},
				},
				{
					description:   "multiple replicas of control plane pods",
					hintAnchor:    "l5d-control-plane-replicas",
					retryDeadline: hc.RetryDeadline,
					warning:       true,
					check: func(ctx context.Context) error {
						if hc.isHA() {
							return hc.checkMinReplicasAvailable(ctx)
						}
						return SkipError{Reason: "not run for non HA installs"}
					},
				},
			},
			false,
		),
	}
}

// CheckProxyVersionsUpToDate checks if all the proxies are on the latest
// installed version
func (hc *HealthChecker) CheckProxyVersionsUpToDate(pods []corev1.Pod) error {
	return CheckProxyVersionsUpToDate(pods, hc.LatestVersions)
}

// CheckProxyVersionsUpToDate checks if all the proxies are on the latest
// installed version
func CheckProxyVersionsUpToDate(pods []corev1.Pod, versions version.Channels) error {
	outdatedPods := []string{}
	for _, pod := range pods {
		status := k8s.GetPodStatus(pod)
		if status == string(corev1.PodRunning) && containsProxy(pod) {
			proxyVersion := k8s.GetProxyVersion(pod)
			if proxyVersion == "" {
				continue
			}
			if err := versions.Match(proxyVersion); err != nil {
				outdatedPods = append(outdatedPods, fmt.Sprintf("\t* %s (%s)", pod.Name, proxyVersion))
			}
		}
	}
	if len(outdatedPods) > 0 {
		podList := strings.Join(outdatedPods, "\n")
		return fmt.Errorf("some proxies are not running the current version:\n%s", podList)
	}
	return nil
}

// CheckIfProxyVersionsMatchWithCLI checks if the latest proxy version
// matches that of the CLI
func CheckIfProxyVersionsMatchWithCLI(pods []corev1.Pod) error {
	for _, pod := range pods {
		proxyVersion := k8s.GetProxyVersion(pod)
		if proxyVersion != "" && proxyVersion != version.Version {
			return fmt.Errorf("%s running %s but cli running %s", pod.Name, proxyVersion, version.Version)
		}
	}
	return nil
}

// CheckCertAndAnchors checks if the given cert and anchors are valid
func (hc *HealthChecker) CheckCertAndAnchors(cert *tls.Cred, trustAnchors []*x509.Certificate, identityName string) error {

	// check anchors time validity
	var expiredAnchors []string
	for _, anchor := range trustAnchors {
		if err := issuercerts.CheckCertValidityPeriod(anchor); err != nil {
			expiredAnchors = append(expiredAnchors, fmt.Sprintf("* %v %s %s", anchor.SerialNumber, anchor.Subject.CommonName, err))
		}
	}
	if len(expiredAnchors) > 0 {
		return fmt.Errorf("anchors not within their validity period:\n\t%s", strings.Join(expiredAnchors, "\n\t"))
	}

	// check cert validity
	if err := issuercerts.CheckCertValidityPeriod(cert.Certificate); err != nil {
		return fmt.Errorf("certificate is %w", err)
	}

	if err := cert.Verify(tls.CertificatesToPool(trustAnchors), identityName, time.Time{}); err != nil {
		return fmt.Errorf("cert is not issued by the trust anchor: %w", err)
	}

	return nil
}

// CheckProxyHealth checks for the data-plane proxies health in the given namespace
// These checks consist of status and identity
func (hc *HealthChecker) CheckProxyHealth(ctx context.Context, controlPlaneNamespace, namespace string) error {
	podList, err := hc.kubeAPI.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: k8s.ControllerNSLabel})
	if err != nil {
		return err
	}

	// Validate the status of the pods
	err = CheckPodsRunning(podList.Items, controlPlaneNamespace)
	if err != nil {
		return err
	}

	// Check proxy certificates
	return checkPodsProxiesCertificate(ctx, *hc.kubeAPI, namespace, controlPlaneNamespace)
}

// CheckCertAndAnchorsExpiringSoon checks if the given cert and anchors expire soon, and returns an
// error if they do.
func (hc *HealthChecker) CheckCertAndAnchorsExpiringSoon(cert *tls.Cred) error {
	// check anchors not expiring soon
	var expiringAnchors []string
	for _, anchor := range cert.TrustChain {
		anchor := anchor
		if err := issuercerts.CheckExpiringSoon(anchor); err != nil {
			expiringAnchors = append(expiringAnchors, fmt.Sprintf("* %v %s %s", anchor.SerialNumber, anchor.Subject.CommonName, err))
		}
	}
	if len(expiringAnchors) > 0 {
		return fmt.Errorf("Anchors expiring soon:\n\t%s", strings.Join(expiringAnchors, "\n\t"))
	}

	// check cert not expiring soon
	if err := issuercerts.CheckExpiringSoon(cert.Certificate); err != nil {
		return fmt.Errorf("certificate %w", err)
	}
	return nil
}

// CheckAPIService checks the status of the given API Service and returns an error if it's not running
func (hc *HealthChecker) CheckAPIService(ctx context.Context, serviceName string) error {
	apiServiceClient, err := apiregistrationv1client.NewForConfig(hc.kubeAPI.Config)
	if err != nil {
		return err
	}

	apiStatus, err := apiServiceClient.APIServices().Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for _, condition := range apiStatus.Status.Conditions {
		if condition.Type == "Available" {
			if condition.Status == "True" {
				return nil
			}
			return fmt.Errorf("%s: %s", condition.Reason, condition.Message)
		}
	}

	return fmt.Errorf("%s service not available", apiStatus.Name)
}

func (hc *HealthChecker) checkMinReplicasAvailable(ctx context.Context) error {
	faulty := []string{}

	for _, component := range linkerdHAControlPlaneComponents {
		conf, err := hc.kubeAPI.AppsV1().Deployments(hc.ControlPlaneNamespace).Get(ctx, component, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if conf.Status.AvailableReplicas <= 1 {
			faulty = append(faulty, component)
		}
	}

	if len(faulty) > 0 {
		return fmt.Errorf("not enough replicas available for %v", faulty)
	}
	return nil
}

// RunChecks runs all configured checkers, and passes the results of each
// check to the observer. If a check fails and is marked as fatal, then all
// remaining checks are skipped. If at least one check fails, RunChecks returns
// false; if all checks passed, RunChecks returns true.  Checks which are
// designated as warnings will not cause RunCheck to return false, however.
func (hc *HealthChecker) RunChecks(observer CheckObserver) (bool, bool) {
	success := true
	warning := false
	for _, c := range hc.categories {
		if c.enabled {
			for _, checker := range c.checkers {
				checker := checker // pin
				if checker.check != nil {
					if !hc.runCheck(c, &checker, observer) {
						if !checker.warning {
							success = false
						} else {
							warning = true
						}
						if checker.fatal {
							return success, warning
						}
					}
				}
			}
		}
	}

	return success, warning
}

// LinkerdConfig gets the Linkerd configuration values.
func (hc *HealthChecker) LinkerdConfig() *l5dcharts.Values {
	return hc.linkerdConfig
}

func (hc *HealthChecker) runCheck(category *Category, c *Checker, observer CheckObserver) bool {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), RequestTimeout)
		err := c.check(ctx)
		cancel()
		var se SkipError
		if errors.As(err, &se) {
			log.Debugf("Skipping check: %s. Reason: %s", c.description, se.Reason)
			return true
		}

		checkResult := &CheckResult{
			Category:    category.ID,
			Description: c.description,
			Warning:     c.warning,
			HintURL:     fmt.Sprintf("%s%s", category.hintBaseURL, c.hintAnchor),
		}
		var vs VerboseSuccess
		if errors.As(err, &vs) {
			checkResult.Description = fmt.Sprintf("%s\n%s", checkResult.Description, vs.Message)
		} else if err != nil {
			checkResult.Err = CategoryError{category.ID, err}
		}

		if checkResult.Err != nil && time.Now().Before(c.retryDeadline) {
			checkResult.Retry = true
			if !c.surfaceErrorOnRetry {
				checkResult.Err = errors.New("waiting for check to complete")
			}
			log.Debugf("Retrying on error: %s", err)

			observer(checkResult)
			time.Sleep(retryWindow)
			continue
		}

		observer(checkResult)
		return checkResult.Err == nil
	}
}

func controlPlaneComponentsSelector() string {
	return fmt.Sprintf("%s,!%s", k8s.ControllerNSLabel, LinkerdCNIResourceLabel)
}

// KubeAPIClient returns a fully configured k8s API client. This client is
// only configured if the KubernetesAPIChecks are configured and run first.
func (hc *HealthChecker) KubeAPIClient() *k8s.KubernetesAPI {
	return hc.kubeAPI
}

// UUID returns the UUID of the installation
func (hc *HealthChecker) UUID() string {
	return hc.uuid
}

func (hc *HealthChecker) checkLinkerdConfigConfigMap(ctx context.Context) (string, *l5dcharts.Values, error) {
	configMap, values, err := FetchCurrentConfiguration(ctx, hc.kubeAPI, hc.ControlPlaneNamespace)
	if err != nil {
		return "", nil, err
	}

	return string(configMap.GetUID()), values, nil
}

// Checks whether the configuration of the linkerd-identity-issuer is correct. This means:
// 1. There is a config map present with identity context
// 2. The scheme in the identity context corresponds to the format of the issuer secret
// 3. The trust anchors (if scheme == kubernetes.io/tls) in the secret equal the ones in config
// 4. The certs and key are parsable
func (hc *HealthChecker) checkCertificatesConfig(ctx context.Context) (*tls.Cred, []*x509.Certificate, error) {
	_, values, err := FetchCurrentConfiguration(ctx, hc.kubeAPI, hc.ControlPlaneNamespace)
	if err != nil {
		return nil, nil, err
	}

	var data *issuercerts.IssuerCertData

	if values.Identity.Issuer.Scheme == "" || values.Identity.Issuer.Scheme == k8s.IdentityIssuerSchemeLinkerd {
		data, err = issuercerts.FetchIssuerData(ctx, hc.kubeAPI, values.IdentityTrustAnchorsPEM, hc.ControlPlaneNamespace)
	} else {
		data, err = issuercerts.FetchExternalIssuerData(ctx, hc.kubeAPI, hc.ControlPlaneNamespace)
	}

	if err != nil {
		return nil, nil, err
	}

	issuerCreds, err := tls.ValidateAndCreateCreds(data.IssuerCrt, data.IssuerKey)
	if err != nil {
		return nil, nil, err
	}

	anchors, err := tls.DecodePEMCertificates(data.TrustAnchors)
	if err != nil {
		return nil, nil, err
	}

	return issuerCreds, anchors, nil
}

// FetchCurrentConfiguration retrieves the current Linkerd configuration
func FetchCurrentConfiguration(ctx context.Context, k kubernetes.Interface, controlPlaneNamespace string) (*corev1.ConfigMap, *l5dcharts.Values, error) {
	// Get the linkerd-config values if present.
	configMap, err := config.FetchLinkerdConfigMap(ctx, k, controlPlaneNamespace)
	if err != nil {
		return nil, nil, err
	}

	rawValues := configMap.Data["values"]
	if rawValues == "" {
		return configMap, nil, nil
	}

	// Convert into latest values, where global field is removed.
	rawValuesBytes, err := config.RemoveGlobalFieldIfPresent([]byte(rawValues))
	if err != nil {
		return nil, nil, err
	}
	rawValues = string(rawValuesBytes)
	var fullValues l5dcharts.Values

	err = yaml.Unmarshal([]byte(rawValues), &fullValues)
	if err != nil {
		return nil, nil, err
	}
	return configMap, &fullValues, nil
}

func (hc *HealthChecker) fetchProxyInjectorCaBundle(ctx context.Context) ([]*x509.Certificate, error) {
	mwh, err := hc.getProxyInjectorMutatingWebhook(ctx)
	if err != nil {
		return nil, err
	}

	caBundle, err := tls.DecodePEMCertificates(string(mwh.ClientConfig.CABundle))
	if err != nil {
		return nil, err
	}
	return caBundle, nil
}

func (hc *HealthChecker) fetchWebhookCaBundle(ctx context.Context, webhook string) ([]*x509.Certificate, error) {
	vwc, err := hc.kubeAPI.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, webhook, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	if len(vwc.Webhooks) != 1 {
		return nil, fmt.Errorf("expected 1 webhooks, found %d", len(vwc.Webhooks))
	}

	caBundle, err := tls.DecodePEMCertificates(string(vwc.Webhooks[0].ClientConfig.CABundle))
	if err != nil {
		return nil, err
	}
	return caBundle, nil
}

// FetchTrustBundle retrieves the ca-bundle from the config-map linkerd-identity-trust-roots
func FetchTrustBundle(ctx context.Context, kubeAPI k8s.KubernetesAPI, controlPlaneNamespace string) (string, error) {
	configMap, err := kubeAPI.CoreV1().ConfigMaps(controlPlaneNamespace).Get(ctx, "linkerd-identity-trust-roots", metav1.GetOptions{})

	return configMap.Data["ca-bundle.crt"], err
}

// FetchCredsFromSecret retrieves the TLS creds given a secret name
func (hc *HealthChecker) FetchCredsFromSecret(ctx context.Context, namespace string, secretName string) (*tls.Cred, error) {
	secret, err := hc.kubeAPI.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	crt, ok := secret.Data[certKeyName]
	if !ok {
		return nil, fmt.Errorf("key %s needs to exist in secret %s", certKeyName, secretName)
	}

	key, ok := secret.Data[keyKeyName]
	if !ok {
		return nil, fmt.Errorf("key %s needs to exist in secret %s", keyKeyName, secretName)
	}

	cred, err := tls.ValidateAndCreateCreds(string(crt), string(key))
	if err != nil {
		return nil, err
	}

	return cred, nil
}

// FetchCredsFromOldSecret function can be removed in later versions, once either all webhook secrets are recreated for each update
// (see https://github.com/linkerd/linkerd2/issues/4813)
// or later releases are only expected to update from the new names.
func (hc *HealthChecker) FetchCredsFromOldSecret(ctx context.Context, namespace string, secretName string) (*tls.Cred, error) {
	secret, err := hc.kubeAPI.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	crt, ok := secret.Data[certOldKeyName]
	if !ok {
		return nil, fmt.Errorf("key %s needs to exist in secret %s", certOldKeyName, secretName)
	}

	key, ok := secret.Data[keyOldKeyName]
	if !ok {
		return nil, fmt.Errorf("key %s needs to exist in secret %s", keyOldKeyName, secretName)
	}

	cred, err := tls.ValidateAndCreateCreds(string(crt), string(key))
	if err != nil {
		return nil, err
	}

	return cred, nil
}

// CheckNamespace checks whether the given namespace exists, and returns an
// error if it does not match `shouldExist`.
func (hc *HealthChecker) CheckNamespace(ctx context.Context, namespace string, shouldExist bool) error {
	exists, err := hc.kubeAPI.NamespaceExists(ctx, namespace)
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

func (hc *HealthChecker) checkClusterNetworks(ctx context.Context) error {
	nodes, err := hc.kubeAPI.GetNodes(ctx)
	if err != nil {
		return err
	}
	clusterNetworks := strings.Split(hc.linkerdConfig.ClusterNetworks, ",")
	clusterIPNets := make([]*net.IPNet, len(clusterNetworks))
	for i, clusterNetwork := range clusterNetworks {
		_, clusterIPNets[i], err = net.ParseCIDR(clusterNetwork)
		if err != nil {
			return err
		}
	}
	var badPodCIDRS []string
	var podCIDRExists bool
	for _, node := range nodes {
		podCIDR := node.Spec.PodCIDR
		if podCIDR == "" {
			continue
		}
		podCIDRExists = true
		podIP, podIPNet, err := net.ParseCIDR(podCIDR)
		if err != nil {
			return err
		}
		exists := cluterNetworksContainCIDR(clusterIPNets, podIPNet, podIP)
		if !exists {
			badPodCIDRS = append(badPodCIDRS, podCIDR)
		}
	}
	// If none of the nodes exposed a podCIDR then we cannot verify the clusterNetworks.
	if !podCIDRExists {
		// DigitalOcean for example, doesn't expose spec.podCIDR (#6398)
		return SkipError{Reason: podCIDRUnavailableSkipReason}
	}
	if len(badPodCIDRS) > 0 {
		sort.Strings(badPodCIDRS)
		return fmt.Errorf("node has podCIDR(s) %v which are not contained in the Linkerd clusterNetworks.\n\tTry installing linkerd via --set clusterNetworks=\"%s\"",
			badPodCIDRS, strings.Join(badPodCIDRS, "\\,"))
	}
	return nil
}

func cluterNetworksContainCIDR(clusterIPNets []*net.IPNet, podIPNet *net.IPNet, podIP net.IP) bool {
	for _, clusterIPNet := range clusterIPNets {
		clusterIPMaskOnes, _ := clusterIPNet.Mask.Size()
		podCIDRMaskOnes, _ := podIPNet.Mask.Size()
		if clusterIPNet.Contains(podIP) && podCIDRMaskOnes >= clusterIPMaskOnes {
			return true
		}
	}
	return false
}

func clusterNetworksContainIP(clusterIPNets []*net.IPNet, ip string) bool {
	for _, clusterIPNet := range clusterIPNets {
		if clusterIPNet.Contains(net.ParseIP(ip)) {
			return true
		}
	}
	return false
}

func (hc *HealthChecker) checkClusterNetworksContainAllPods(ctx context.Context) error {
	clusterNetworks := strings.Split(hc.linkerdConfig.ClusterNetworks, ",")
	clusterIPNets := make([]*net.IPNet, len(clusterNetworks))
	var err error
	for i, clusterNetwork := range clusterNetworks {
		_, clusterIPNets[i], err = net.ParseCIDR(clusterNetwork)
		if err != nil {
			return err
		}
	}
	pods, err := hc.kubeAPI.CoreV1().Pods(corev1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, pod := range pods.Items {
		if pod.Spec.HostNetwork {
			continue
		}
		if len(pod.Status.PodIP) == 0 {
			continue
		}
		if !clusterNetworksContainIP(clusterIPNets, pod.Status.PodIP) {
			return fmt.Errorf("the Linkerd clusterNetworks [%q] do not include pod %s/%s (%s)", hc.linkerdConfig.ClusterNetworks, pod.Namespace, pod.Name, pod.Status.PodIP)
		}
	}
	return nil
}

func (hc *HealthChecker) checkClusterNetworksContainAllServices(ctx context.Context) error {
	clusterNetworks := strings.Split(hc.linkerdConfig.ClusterNetworks, ",")
	clusterIPNets := make([]*net.IPNet, len(clusterNetworks))
	var err error
	for i, clusterNetwork := range clusterNetworks {
		_, clusterIPNets[i], err = net.ParseCIDR(clusterNetwork)
		if err != nil {
			return err
		}
	}
	svcs, err := hc.kubeAPI.CoreV1().Services(corev1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, svc := range svcs.Items {
		clusterIP := svc.Spec.ClusterIP
		if clusterIP != "" && clusterIP != "None" && !clusterNetworksContainIP(clusterIPNets, svc.Spec.ClusterIP) {
			return fmt.Errorf("the Linkerd clusterNetworks [%q] do not include svc %s/%s (%s)", hc.linkerdConfig.ClusterNetworks, svc.Namespace, svc.Name, svc.Spec.ClusterIP)
		}
	}
	return nil
}

func (hc *HealthChecker) expectedRBACNames() []string {
	return []string{
		fmt.Sprintf("linkerd-%s-identity", hc.ControlPlaneNamespace),
		fmt.Sprintf("linkerd-%s-proxy-injector", hc.ControlPlaneNamespace),
	}
}

func (hc *HealthChecker) checkClusterRoles(ctx context.Context, shouldExist bool, expectedNames []string, labelSelector string) error {
	return CheckClusterRoles(ctx, hc.kubeAPI, shouldExist, expectedNames, labelSelector)
}

// CheckClusterRoles checks that the expected ClusterRoles exist.
func CheckClusterRoles(ctx context.Context, kubeAPI *k8s.KubernetesAPI, shouldExist bool, expectedNames []string, labelSelector string) error {
	options := metav1.ListOptions{
		LabelSelector: labelSelector,
	}
	crList, err := kubeAPI.RbacV1().ClusterRoles().List(ctx, options)
	if err != nil {
		return err
	}

	objects := []runtime.Object{}

	for _, item := range crList.Items {
		item := item // pin
		objects = append(objects, &item)
	}

	return checkResources("ClusterRoles", objects, expectedNames, shouldExist)
}

func (hc *HealthChecker) checkClusterRoleBindings(ctx context.Context, shouldExist bool, expectedNames []string, labelSelector string) error {
	return CheckClusterRoleBindings(ctx, hc.kubeAPI, shouldExist, expectedNames, labelSelector)
}

// CheckClusterRoleBindings checks that the expected ClusterRoleBindings exist.
func CheckClusterRoleBindings(ctx context.Context, kubeAPI *k8s.KubernetesAPI, shouldExist bool, expectedNames []string, labelSelector string) error {
	options := metav1.ListOptions{
		LabelSelector: labelSelector,
	}
	crbList, err := kubeAPI.RbacV1().ClusterRoleBindings().List(ctx, options)
	if err != nil {
		return err
	}

	objects := []runtime.Object{}

	for _, item := range crbList.Items {
		item := item // pin
		objects = append(objects, &item)
	}

	return checkResources("ClusterRoleBindings", objects, expectedNames, shouldExist)
}

// CheckConfigMaps checks that the expected ConfigMaps  exist.
func CheckConfigMaps(ctx context.Context, kubeAPI *k8s.KubernetesAPI, namespace string, shouldExist bool, expectedNames []string, labelSelector string) error {
	options := metav1.ListOptions{
		LabelSelector: labelSelector,
	}
	crbList, err := kubeAPI.CoreV1().ConfigMaps(namespace).List(ctx, options)
	if err != nil {
		return err
	}

	objects := []runtime.Object{}

	for _, item := range crbList.Items {
		item := item // pin
		objects = append(objects, &item)
	}

	return checkResources("ConfigMaps", objects, expectedNames, shouldExist)
}

func (hc *HealthChecker) isHA() bool {
	return hc.linkerdConfig.HighAvailability
}

func (hc *HealthChecker) isHeartbeatDisabled() bool {
	return hc.linkerdConfig.DisableHeartBeat
}

func (hc *HealthChecker) checkServiceAccounts(ctx context.Context, saNames []string, ns, labelSelector string) error {
	return CheckServiceAccounts(ctx, hc.kubeAPI, saNames, ns, labelSelector)
}

// CheckServiceAccounts check for serviceaccounts
func CheckServiceAccounts(ctx context.Context, api *k8s.KubernetesAPI, saNames []string, ns, labelSelector string) error {
	options := metav1.ListOptions{
		LabelSelector: labelSelector,
	}
	saList, err := api.CoreV1().ServiceAccounts(ns).List(ctx, options)
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

// CheckIfLinkerdExists checks if Linkerd exists
func CheckIfLinkerdExists(ctx context.Context, kubeAPI *k8s.KubernetesAPI, controlPlaneNamespace string) (bool, error) {
	_, err := kubeAPI.CoreV1().Namespaces().Get(ctx, controlPlaneNamespace, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	_, _, err = FetchCurrentConfiguration(ctx, kubeAPI, controlPlaneNamespace)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (hc *HealthChecker) getProxyInjectorMutatingWebhook(ctx context.Context) (*admissionRegistration.MutatingWebhook, error) {
	mwc, err := hc.kubeAPI.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, k8s.ProxyInjectorWebhookConfigName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if len(mwc.Webhooks) != 1 {
		return nil, fmt.Errorf("expected 1 webhooks, found %d", len(mwc.Webhooks))
	}
	return &mwc.Webhooks[0], nil
}

func (hc *HealthChecker) getMutatingWebhookFailurePolicy(ctx context.Context) (*admissionRegistration.FailurePolicyType, error) {
	mwh, err := hc.getProxyInjectorMutatingWebhook(ctx)
	if err != nil {
		return nil, err
	}
	return mwh.FailurePolicy, nil
}

func (hc *HealthChecker) checkMutatingWebhookConfigurations(ctx context.Context, shouldExist bool) error {
	options := metav1.ListOptions{
		LabelSelector: controlPlaneComponentsSelector(),
	}
	mwc, err := hc.kubeAPI.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, options)
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

func (hc *HealthChecker) checkValidatingWebhookConfigurations(ctx context.Context, shouldExist bool) error {
	options := metav1.ListOptions{
		LabelSelector: controlPlaneComponentsSelector(),
	}
	vwc, err := hc.kubeAPI.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(ctx, options)
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

// CheckCustomResourceDefinitions checks that all of the Linkerd CRDs are
// installed on the cluster.
func CheckCustomResourceDefinitions(ctx context.Context, k8sAPI *k8s.KubernetesAPI, expectedCRDManifests string) error {

	crdYamls := strings.Split(expectedCRDManifests, "---\n")
	crdVersions := []struct{ name, version string }{}
	for _, crdYaml := range crdYamls {
		var crd apiextv1.CustomResourceDefinition
		err := yaml.Unmarshal([]byte(crdYaml), &crd)
		if err != nil {
			return err
		}
		if len(crd.Spec.Versions) == 0 {
			continue
		}
		versionIndex := len(crd.Spec.Versions) - 1
		crdVersions = append(crdVersions, struct{ name, version string }{
			name:    crd.Name,
			version: crd.Spec.Versions[versionIndex].Name,
		})
	}

	errMsgs := []string{}

	for _, crdVersion := range crdVersions {
		name := crdVersion.name
		version := crdVersion.version

		crd, err := k8sAPI.Apiextensions.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
		if err != nil && kerrors.IsNotFound(err) {
			errMsgs = append(errMsgs, fmt.Sprintf("missing %s", name))
			continue
		} else if err != nil {
			return err
		}
		if !crdHasVersion(crd, version) {
			errMsgs = append(errMsgs, fmt.Sprintf("CRD %s is missing version %s", name, version))
		}
	}
	if len(errMsgs) > 0 {
		return errors.New(strings.Join(errMsgs, ", "))
	}
	return nil
}

func crdHasVersion(crd *v1.CustomResourceDefinition, version string) bool {
	for _, crdVersion := range crd.Spec.Versions {
		if crdVersion.Name == version {
			return true
		}
	}
	return false
}

// CheckNodesHaveNonDockerRuntime checks that each node has a non-Docker
// runtime. This check is only called if proxyInit is not running as root
// which is a problem for clusters with a Docker container runtime.
func CheckNodesHaveNonDockerRuntime(ctx context.Context, k8sAPI *k8s.KubernetesAPI) error {
	hasDockerNodes := false
	continueToken := ""
	for {
		nodes, err := k8sAPI.CoreV1().Nodes().List(ctx, metav1.ListOptions{Continue: continueToken})
		if err != nil {
			return err
		}
		continueToken = nodes.Continue
		for _, node := range nodes.Items {
			crv := node.Status.NodeInfo.ContainerRuntimeVersion
			if strings.HasPrefix(crv, "docker:") {
				hasDockerNodes = true
				break
			}
		}
		if continueToken == "" {
			break
		}
	}
	if hasDockerNodes {
		return fmt.Errorf("there are nodes using the docker container runtime and proxy-init container must run as root user.\ntry installing linkerd via --set proxyInit.runAsRoot=true")
	}
	return nil
}

// MeshedPodIdentityData contains meshed pod details + trust anchors of the proxy
type MeshedPodIdentityData struct {
	Name      string
	Namespace string
	Anchors   string
}

// GetMeshedPodsIdentityData obtains the identity data (trust anchors) for all meshed pods
func GetMeshedPodsIdentityData(ctx context.Context, api kubernetes.Interface, dataPlaneNamespace string) ([]MeshedPodIdentityData, error) {
	podList, err := api.CoreV1().Pods(dataPlaneNamespace).List(ctx, metav1.ListOptions{LabelSelector: k8s.ControllerNSLabel})
	if err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, nil
	}
	pods := []MeshedPodIdentityData{}
	for _, pod := range podList.Items {
		for _, containerSpec := range pod.Spec.Containers {
			if containerSpec.Name != k8s.ProxyContainerName {
				continue
			}
			for _, envVar := range containerSpec.Env {
				if envVar.Name != identity.EnvTrustAnchors {
					continue
				}
				pods = append(pods, MeshedPodIdentityData{
					pod.Name, pod.Namespace, strings.TrimSpace(envVar.Value),
				})
			}
		}
	}
	return pods, nil
}

func (hc *HealthChecker) checkDataPlaneProxiesCertificate(ctx context.Context) error {
	return checkPodsProxiesCertificate(ctx, *hc.kubeAPI, hc.DataPlaneNamespace, hc.ControlPlaneNamespace)
}

func checkPodsProxiesCertificate(ctx context.Context, kubeAPI k8s.KubernetesAPI, targetNamespace, controlPlaneNamespace string) error {
	meshedPods, err := GetMeshedPodsIdentityData(ctx, kubeAPI, targetNamespace)
	if err != nil {
		return err
	}

	trustAnchorsPem, err := FetchTrustBundle(ctx, kubeAPI, controlPlaneNamespace)
	if err != nil {
		return err
	}

	offendingPods := []string{}
	for _, pod := range meshedPods {
		// Skip control plane pods since they load their trust anchors from the linkerd-identity-trust-anchors configmap.
		if pod.Namespace == controlPlaneNamespace {
			continue
		}
		if strings.TrimSpace(pod.Anchors) != strings.TrimSpace(trustAnchorsPem) {
			if targetNamespace == "" {
				offendingPods = append(offendingPods, fmt.Sprintf("* %s/%s", pod.Namespace, pod.Name))
			} else {
				offendingPods = append(offendingPods, fmt.Sprintf("* %s", pod.Name))
			}
		}
	}
	if len(offendingPods) == 0 {
		return nil
	}
	return fmt.Errorf("Some pods do not have the current trust bundle and must be restarted:\n\t%s", strings.Join(offendingPods, "\n\t"))
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
			return ResourceError{resourceName, resources}
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

// Check if there's a pod with the "opaque ports" annotation defined but a
// service selecting the aforementioned pod doesn't define it
func (hc *HealthChecker) checkMisconfiguredOpaquePortAnnotations(ctx context.Context) error {
	// Initialize and sync the kubernetes API
	// This is used instead of `hc.kubeAPI` to limit multiple k8s API requests
	// and use the caching logic in the shared informers
	// TODO: move the shared informer code out of `controller/`, and into `pkg` to simplify the dependency tree.
	kubeAPI := controllerK8s.NewClusterScopedAPI(hc.kubeAPI, nil, nil, controllerK8s.Endpoint, controllerK8s.Pod, controllerK8s.Svc)
	kubeAPI.Sync(ctx.Done())

	services, err := kubeAPI.Svc().Lister().Services(hc.DataPlaneNamespace).List(labels.Everything())
	if err != nil {
		return err
	}

	var errStrings []string
	for _, service := range services {
		if service.Spec.ClusterIP == "None" {
			// skip headless services; they're handled differently
			continue
		}

		endpoints, err := kubeAPI.Endpoint().Lister().Endpoints(service.Namespace).Get(service.Name)
		if err != nil {
			return err
		}

		pods, err := getEndpointsPods(endpoints, kubeAPI, service.Namespace)
		if err != nil {
			return err
		}

		for pod := range pods {
			err := misconfiguredOpaqueAnnotation(service, pod)
			if err != nil {
				errStrings = append(errStrings, fmt.Sprintf("\t* %s", err.Error()))
			}
		}
	}

	if len(errStrings) >= 1 {
		return fmt.Errorf(strings.Join(errStrings, "\n    "))
	}

	return nil
}

// getEndpointsPods takes a collection of endpoints and returns the set of all
// the pods that they target.
func getEndpointsPods(endpoints *corev1.Endpoints, kubeAPI *controllerK8s.API, namespace string) (map[*corev1.Pod]struct{}, error) {
	pods := make(map[*corev1.Pod]struct{})
	for _, subset := range endpoints.Subsets {
		for _, addr := range subset.Addresses {
			if addr.TargetRef != nil && addr.TargetRef.Kind == "Pod" {
				pod, err := kubeAPI.Pod().Lister().Pods(namespace).Get(addr.TargetRef.Name)
				if err != nil {
					return nil, err
				}
				if _, ok := pods[pod]; !ok {
					pods[pod] = struct{}{}
				}
			}
		}
	}
	return pods, nil
}

func misconfiguredOpaqueAnnotation(service *corev1.Service, pod *corev1.Pod) error {
	var svcPorts, podPorts []string
	if v, ok := service.Annotations[k8s.ProxyOpaquePortsAnnotation]; ok {
		svcPorts = strings.Split(v, ",")
	}
	if v, ok := pod.Annotations[k8s.ProxyOpaquePortsAnnotation]; ok {
		podPorts = strings.Split(v, ",")
	}

	// First loop through the services opaque ports and assert that if the pod
	// exposes a port that is targeted by one of these ports, then it is
	// marked as opaque on the pod.
	for _, p := range svcPorts {
		port, err := strconv.Atoi(p)
		if err != nil {
			return fmt.Errorf("failed to convert %s to port number for pod %s", p, pod.Name)
		}
		err = checkPodPorts(service, pod, podPorts, port)
		if err != nil {
			return err
		}
	}

	// Next loop through the pod's opaque ports and assert that if one of
	// the ports is targeted by a service port, then it is marked as opaque
	// on the service.
	for _, p := range podPorts {
		if util.ContainsString(p, svcPorts) {
			// The service exposes p and is marked as opaque.
			continue
		}
		port, err := strconv.Atoi(p)
		if err != nil {
			return fmt.Errorf("failed to convert %s to port number for pod %s", p, pod.Name)
		}

		// p is marked as opaque on the pod, but the service that selects it
		// does not have it marked as opaque. We first check if the service
		// exposes it as a service or integer targetPort.
		ok, err := checkServiceIntPorts(service, svcPorts, port)
		if err != nil {
			return err
		}
		if ok {
			// The service targets the port as an integer and is marked as
			// opaque so continue checking other pod ports.
			continue
		}

		// The service does not expose p as a service or integer targetPort.
		// We now check if it targets it as a named port, and if so, that the
		// service port is marked as opaque.
		err = checkServiceNamePorts(service, pod, port, svcPorts)
		if err != nil {
			return err
		}
	}
	return nil
}

func checkPodPorts(service *corev1.Service, pod *corev1.Pod, podPorts []string, port int) error {
	for _, sp := range service.Spec.Ports {
		if int(sp.Port) == port {
			for _, c := range pod.Spec.Containers {
				for _, cp := range c.Ports {
					if cp.ContainerPort == sp.TargetPort.IntVal || cp.Name == sp.TargetPort.StrVal {
						// The pod exposes a container port that would be
						// targeted by this service port
						var strPort string
						if sp.TargetPort.Type == 0 {
							strPort = strconv.Itoa(int(sp.TargetPort.IntVal))
						} else {
							strPort = strconv.Itoa(int(cp.ContainerPort))
						}
						if util.ContainsString(strPort, podPorts) {
							return nil
						}
						return fmt.Errorf("service %s expects target port %s to be opaque; add it to pod %s %s annotation", service.Name, strPort, pod.Name, k8s.ProxyOpaquePortsAnnotation)
					}
				}
			}
		}
	}
	return nil
}

func checkServiceIntPorts(service *corev1.Service, svcPorts []string, port int) (bool, error) {
	for _, p := range service.Spec.Ports {
		if p.TargetPort.Type == 0 && p.TargetPort.IntVal == 0 {
			if int(p.Port) == port {
				// The service does not have a target port, so its service
				// port should be marked as opaque.
				return false, fmt.Errorf("service %s targets the opaque port %d; add it to its %s annotation", service.Name, port, k8s.ProxyOpaquePortsAnnotation)
			}
		}
		if int(p.TargetPort.IntVal) == port {
			svcPort := strconv.Itoa(int(p.Port))
			if util.ContainsString(svcPort, svcPorts) {
				// The service exposes svcPort which targets p and svcPort
				// is properly as opaque.
				return true, nil
			}
			return false, fmt.Errorf("service %s targets the opaque port %d through %d; add %d to its %s annotation", service.Name, port, p.Port, p.Port, k8s.ProxyOpaquePortsAnnotation)
		}
	}
	return false, nil
}

func checkServiceNamePorts(service *corev1.Service, pod *corev1.Pod, port int, svcPorts []string) error {
	for _, p := range service.Spec.Ports {
		if p.TargetPort.StrVal == "" {
			// The target port is not named so there is no named container
			// port to check.
			continue
		}
		for _, c := range pod.Spec.Containers {
			for _, cp := range c.Ports {
				if int(cp.ContainerPort) == port {
					// This is the containerPort that maps to the opaque port
					// we are currently checking.
					if cp.Name == p.TargetPort.StrVal {
						svcPort := strconv.Itoa(int(p.Port))
						if util.ContainsString(svcPort, svcPorts) {
							// The service targets the container port by name
							// and is marked as opaque.
							return nil
						}
						return fmt.Errorf("service %s targets the opaque port %s through %d; add %d to its %s annotation", service.Name, cp.Name, p.Port, p.Port, k8s.ProxyOpaquePortsAnnotation)
					}
				}
			}
		}
	}
	return nil
}

// GetDataPlanePods returns all the pods with data plane
func (hc *HealthChecker) GetDataPlanePods(ctx context.Context) ([]corev1.Pod, error) {
	selector := fmt.Sprintf("%s=%s", k8s.ControllerNSLabel, hc.ControlPlaneNamespace)
	podList, err := hc.kubeAPI.CoreV1().Pods(hc.DataPlaneNamespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	return podList.Items, nil
}

// GetServices returns all services within data plane namespace
func (hc *HealthChecker) GetServices(ctx context.Context) ([]corev1.Service, error) {
	svcList, err := hc.kubeAPI.CoreV1().Services(hc.DataPlaneNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return svcList.Items, nil
}

func (hc *HealthChecker) checkHAMetadataPresentOnKubeSystemNamespace(ctx context.Context) error {
	ns, err := hc.kubeAPI.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil {
		return err
	}

	val, ok := ns.Labels[k8s.AdmissionWebhookLabel]
	if !ok || val != "disabled" {
		return fmt.Errorf("kube-system namespace needs to have the label %s: disabled if injector webhook failure policy is Fail", k8s.AdmissionWebhookLabel)
	}

	return nil
}

func (hc *HealthChecker) checkCanCreate(ctx context.Context, namespace, group, version, resource string) error {
	return CheckCanPerformAction(ctx, hc.kubeAPI, "create", namespace, group, version, resource)
}

func (hc *HealthChecker) checkCanCreateNonNamespacedResources(ctx context.Context) error {
	var errs []string
	dryRun := metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}

	// Iterate over all resources in install manifest
	installManifestReader := strings.NewReader(hc.Options.InstallManifest)
	yamlReader := yamlDecoder.NewYAMLReader(bufio.NewReader(installManifestReader))
	for {
		// Read single object YAML
		objYAML, err := yamlReader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("error reading install manifest: %w", err)
		}

		// Create unstructured object from YAML
		objMap := map[string]interface{}{}
		err = yaml.Unmarshal(objYAML, &objMap)
		if err != nil {
			return fmt.Errorf("error unmarshaling yaml object %s: %w", objYAML, err)
		}
		if len(objMap) == 0 {
			// Ignore header blocks with only comments
			continue
		}
		obj := &unstructured.Unstructured{Object: objMap}

		// Skip namespaced resources (dry-run requires namespace to exist)
		if obj.GetNamespace() != "" {
			continue
		}
		// Attempt to create resource using dry-run
		resource, _ := meta.UnsafeGuessKindToResource(obj.GroupVersionKind())
		_, err = hc.kubeAPI.DynamicClient.Resource(resource).Create(ctx, obj, dryRun)
		if err != nil {
			errs = append(errs, fmt.Sprintf("cannot create %s/%s: %v", obj.GetKind(), obj.GetName(), err))
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n    "))
	}
	return nil
}

func (hc *HealthChecker) checkCanGet(ctx context.Context, namespace, group, version, resource string) error {
	return CheckCanPerformAction(ctx, hc.kubeAPI, "get", namespace, group, version, resource)
}

func (hc *HealthChecker) checkExtensionAPIServerAuthentication(ctx context.Context) error {
	if hc.kubeAPI == nil {
		return fmt.Errorf("unexpected error: Kubernetes ClientSet not initialized")
	}
	m, err := hc.kubeAPI.CoreV1().ConfigMaps(metav1.NamespaceSystem).Get(ctx, k8s.ExtensionAPIServerAuthenticationConfigMapName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if v, exists := m.Data[k8s.ExtensionAPIServerAuthenticationRequestHeaderClientCAFileKey]; !exists || v == "" {
		return fmt.Errorf("--%s is not configured", k8s.ExtensionAPIServerAuthenticationRequestHeaderClientCAFileKey)
	}
	return nil
}
func (hc *HealthChecker) checkClockSkew(ctx context.Context) error {
	if hc.kubeAPI == nil {
		// we should never get here
		return fmt.Errorf("unexpected error: Kubernetes ClientSet not initialized")
	}

	var clockSkewNodes []string

	nodeList, err := hc.kubeAPI.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
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

// CheckRoles checks that the expected roles exist.
func CheckRoles(ctx context.Context, kubeAPI *k8s.KubernetesAPI, shouldExist bool, namespace string, expectedNames []string, labelSelector string) error {
	options := metav1.ListOptions{
		LabelSelector: labelSelector,
	}
	crList, err := kubeAPI.RbacV1().Roles(namespace).List(ctx, options)
	if err != nil {
		return err
	}

	objects := []runtime.Object{}

	for _, item := range crList.Items {
		item := item // pin
		objects = append(objects, &item)
	}

	return checkResources("Roles", objects, expectedNames, shouldExist)
}

// CheckRoleBindings checks that the expected RoleBindings exist.
func CheckRoleBindings(ctx context.Context, kubeAPI *k8s.KubernetesAPI, shouldExist bool, namespace string, expectedNames []string, labelSelector string) error {
	options := metav1.ListOptions{
		LabelSelector: labelSelector,
	}
	crbList, err := kubeAPI.RbacV1().RoleBindings(namespace).List(ctx, options)
	if err != nil {
		return err
	}

	objects := []runtime.Object{}

	for _, item := range crbList.Items {
		item := item // pin
		objects = append(objects, &item)
	}

	return checkResources("RoleBindings", objects, expectedNames, shouldExist)
}

// CheckCanPerformAction checks if a given k8s client is authorized to perform a given action.
func CheckCanPerformAction(ctx context.Context, api *k8s.KubernetesAPI, verb, namespace, group, version, resource string) error {
	if api == nil {
		// we should never get here
		return fmt.Errorf("unexpected error: Kubernetes ClientSet not initialized")
	}

	return k8s.ResourceAuthz(
		ctx,
		api,
		namespace,
		verb,
		group,
		version,
		resource,
		"",
	)
}

// getPodStatuses returns a map of all Linkerd container statuses:
// component =>
//   pod name =>
//     container statuses
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

func validateControlPlanePods(pods []corev1.Pod) error {
	statuses := getPodStatuses(pods)

	names := []string{"destination", "identity", "proxy-injector"}

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
					// TODO: Save this as a warning, allow check to pass but let the user
					// know there is at least one pod not ready. This might imply
					// restructuring health checks to allow individual checks to return
					// either fatal or warning, rather than setting this property at
					// compile time.
					err = fmt.Errorf("pod/%s container %s is not ready", pod, container.Name)
					containersReady = false
				}
			}
			if containersReady {
				// at least one pod has all containers ready
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

func checkUnschedulablePods(pods []corev1.Pod) error {
	for _, pod := range pods {
		for _, condition := range pod.Status.Conditions {
			if condition.Reason == corev1.PodReasonUnschedulable {
				return fmt.Errorf("%s: %s", pod.Name, condition.Message)
			}
		}
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

// CheckForPods checks if the given deployments have pod resources present
func CheckForPods(pods []corev1.Pod, deployNames []string) error {
	exists := make(map[string]bool)

	for _, pod := range pods {
		for label, value := range pod.Labels {
			// When the label value is `linkerd.io/control-plane-component` or
			// `component`, we'll take its value as the name of the deployment
			// that the pod is part of
			if label == k8s.ControllerComponentLabel || label == "component" {
				exists[value] = true
			}
		}
	}

	for _, expected := range deployNames {
		if !exists[expected] {
			return fmt.Errorf("Could not find pods for deployment %s", expected)
		}
	}

	return nil
}

// CheckPodsRunning checks if the given pods are in running state
// along with containers to be in ready state
func CheckPodsRunning(pods []corev1.Pod, namespace string) error {
	if len(pods) == 0 {
		msg := fmt.Sprintf("no \"%s\" containers found", k8s.ProxyContainerName)
		if namespace != "" {
			msg += fmt.Sprintf(" in the \"%s\" namespace", namespace)
		}
		return fmt.Errorf(msg)
	}
	for _, pod := range pods {
		status := k8s.GetPodStatus(pod)

		// Skip validating pods that have a status which indicates there would
		// be no running proxy container.
		switch status {
		case "Completed", "NodeShutdown", "Shutdown", "Terminated":
			continue
		}
		if status != string(corev1.PodRunning) && status != "Evicted" {
			return fmt.Errorf("pod \"%s\" status is %s", pod.Name, pod.Status.Phase)
		}
		if !k8s.GetProxyReady(pod) {
			return fmt.Errorf("container \"%s\" in pod \"%s\" is not ready", k8s.ProxyContainerName, pod.Name)
		}
	}
	return nil
}

// CheckIfDataPlanePodsExist checks if the proxy is present in the given pods
func CheckIfDataPlanePodsExist(pods []corev1.Pod) error {
	for _, pod := range pods {
		if !containsProxy(pod) {
			return fmt.Errorf("could not find proxy container for %s pod", pod.Name)
		}
	}
	return nil
}

func containsProxy(pod corev1.Pod) bool {
	for _, containerSpec := range pod.Spec.Containers {
		if containerSpec.Name == k8s.ProxyContainerName {
			return true
		}
	}
	return false
}
