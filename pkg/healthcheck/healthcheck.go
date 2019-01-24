package healthcheck

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/controller/api/public"
	spclient "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/profiles"
	"github.com/linkerd/linkerd2/pkg/version"
	authorizationapi "k8s.io/api/authorization/v1beta1"
	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sVersion "k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
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

	// LinkerdPreInstallClusterChecks adds checks to validate that the control
	// plane namespace does not already exist, and that the user can create
	// cluster-wide resources, including ClusterRole, ClusterRoleBinding, and
	// CustomResourceDefinition. This check only runs as part of the set
	// of pre-install checks.
	// This check is dependent on the output of KubernetesAPIChecks, so those
	// checks must be added first.
	LinkerdPreInstallClusterChecks CategoryID = "pre-kubernetes-cluster-setup"

	// LinkerdPreInstallSingleNamespaceChecks adds a check to validate that the
	// control plane namespace already exists, and that the user can create
	// namespace-scoped resources, including Role and RoleBinding. This check only
	// runs as part of the set of pre-install checks.
	// This check is dependent on the output of KubernetesAPIChecks, so those
	// checks must be added first.
	LinkerdPreInstallSingleNamespaceChecks CategoryID = "pre-kubernetes-single-namespace-setup"

	// LinkerdPreInstallChecks adds checks to validate that the user can create
	// Kubernetes objects necessary to install the control plane, including
	// Service, Deployment, and ConfigMap. This check only runs as part of the set
	// of pre-install checks.
	// This check is dependent on the output of KubernetesAPIChecks, so those
	// checks must be added first.
	LinkerdPreInstallChecks CategoryID = "pre-kubernetes-setup"

	// LinkerdControlPlaneExistenceChecks adds a series of checks to validate that
	// the control plane namespace and controller pod exist.
	// These checks are dependent on the output of KubernetesAPIChecks, so those
	// checks must be added first.
	LinkerdControlPlaneExistenceChecks CategoryID = "linkerd-existence"

	// LinkerdAPIChecks adds a series of checks to validate that the control plane
	// is successfully serving the public API.
	// These checks are dependent on the output of KubernetesAPIChecks, so those
	// checks must be added first.
	LinkerdAPIChecks CategoryID = "linkerd-api"

	// LinkerdServiceProfileChecks add a check validate any ServiceProfiles that
	// may already be installed.
	// These checks are dependent on the output of KubernetesAPIChecks, so those
	// checks must be added first.
	LinkerdServiceProfileChecks CategoryID = "linkerd-service-profile"

	// LinkerdVersionChecks adds a series of checks to query for the latest
	// version, and validate the the CLI is up to date.
	LinkerdVersionChecks CategoryID = "linkerd-version"

	// LinkerdControlPlaneVersionChecks adds a series of checks to validate that
	// the control plane is running the latest available version.
	// These checks are dependent on `apiClient` from
	// LinkerdControlPlaneExistenceChecks and `latestVersion` from
	// LinkerdVersionChecks, so those checks must be added first.
	LinkerdControlPlaneVersionChecks CategoryID = "control-plane-version"

	// LinkerdDataPlaneChecks adds data plane checks to validate that the data
	// plane namespace exists, and that the the proxy containers are in a ready
	// state and running the latest available version.
	// These checks are dependent on the output of KubernetesAPIChecks,
	// `apiClient` from LinkerdControlPlaneExistenceChecks, and `latestVersion`
	// from LinkerdVersionChecks, so those checks must be added first.
	LinkerdDataPlaneChecks CategoryID = "linkerd-data-plane"
)

var (
	maxRetries        = 60
	retryWindow       = 5 * time.Second
	clusterZoneSuffix = []string{"svc", "cluster", "local"}
)

type checker struct {
	// description is the short description that's printed to the command line
	// when the check is executed
	description string

	// hintURL provides a pointer to more information about the check
	hintURL string

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

	// check is the function that's called to execute the check; if the function
	// returns an error, the check fails
	check func() error

	// checkRPC is an alternative to check that can be used to perform a remote
	// check using the SelfCheck gRPC endpoint; check status is based on the value
	// of the gRPC response
	checkRPC func() (*healthcheckPb.SelfCheckResponse, error)
}

// CheckResult encapsulates a check's identifying information and output
type CheckResult struct {
	Category    CategoryID
	Description string
	HintURL     string
	Retry       bool
	Warning     bool
	Err         error
}

type checkObserver func(*CheckResult)

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
	APIAddr               string
	VersionOverride       string
	RetryDeadline         time.Time
}

// HealthChecker encapsulates all health check checkers, and clients required to
// perform those checks.
type HealthChecker struct {
	categories []category
	*Options

	// these fields are set in the process of running checks
	kubeAPI          *k8s.KubernetesAPI
	httpClient       *http.Client
	clientset        *kubernetes.Clientset
	spClientset      *spclient.Clientset
	kubeVersion      *k8sVersion.Info
	controlPlanePods []v1.Pod
	apiClient        pb.ApiClient
	latestVersion    string
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
func (hc *HealthChecker) allCategories() []category {
	return []category{
		{
			id: KubernetesAPIChecks,
			checkers: []checker{
				{
					description: "can initialize the client",
					fatal:       true,
					check: func() (err error) {
						hc.kubeAPI, err = k8s.NewAPI(hc.KubeConfig, hc.KubeContext)
						return
					},
				},
				{
					description: "can query the Kubernetes API",
					fatal:       true,
					check: func() (err error) {
						hc.httpClient, err = hc.kubeAPI.NewClient()
						if err != nil {
							return
						}
						hc.kubeVersion, err = hc.kubeAPI.GetVersionInfo(hc.httpClient)
						return
					},
				},
			},
		},
		{
			id: KubernetesVersionChecks,
			checkers: []checker{
				{
					description: "is running the minimum Kubernetes API version",
					check: func() error {
						return hc.kubeAPI.CheckVersion(hc.kubeVersion)
					},
				},
			},
		},
		{
			id: LinkerdPreInstallClusterChecks,
			checkers: []checker{
				{
					description: "control plane namespace does not already exist",
					check: func() error {
						return hc.checkNamespace(hc.ControlPlaneNamespace, false)
					},
				},
				{
					description: "can create Namespaces",
					check: func() error {
						return hc.checkCanCreate("", "", "v1", "Namespace")
					},
				},
				{
					description: "can create ClusterRoles",
					hintURL:     "https://linkerd.io/2/getting-started/#step-0-setup",
					check: func() error {
						return hc.checkCanCreate("", "rbac.authorization.k8s.io", "v1beta1", "ClusterRole")
					},
				},
				{
					description: "can create ClusterRoleBindings",
					hintURL:     "https://linkerd.io/2/getting-started/#step-0-setup",
					check: func() error {
						return hc.checkCanCreate("", "rbac.authorization.k8s.io", "v1beta1", "ClusterRoleBinding")
					},
				},
				{
					description: "can create CustomResourceDefinitions",
					check: func() error {
						return hc.checkCanCreate(hc.ControlPlaneNamespace, "apiextensions.k8s.io", "v1beta1", "CustomResourceDefinition")
					},
				},
			},
		},
		{
			id: LinkerdPreInstallSingleNamespaceChecks,
			checkers: []checker{
				{
					description: "control plane namespace exists",
					check: func() error {
						return hc.checkNamespace(hc.ControlPlaneNamespace, true)
					},
				},
				{
					description: "can create Roles",
					hintURL:     "https://linkerd.io/2/getting-started/#step-0-setup",
					check: func() error {
						return hc.checkCanCreate("", "rbac.authorization.k8s.io", "v1beta1", "Role")
					},
				},
				{
					description: "can create RoleBindings",
					hintURL:     "https://linkerd.io/2/getting-started/#step-0-setup",
					check: func() error {
						return hc.checkCanCreate("", "rbac.authorization.k8s.io", "v1beta1", "RoleBinding")
					},
				},
			},
		},
		{
			id: LinkerdPreInstallChecks,
			checkers: []checker{
				{
					description: "can create ServiceAccounts",
					check: func() error {
						return hc.checkCanCreate(hc.ControlPlaneNamespace, "", "v1", "ServiceAccount")
					},
				},
				{
					description: "can create Services",
					check: func() error {
						return hc.checkCanCreate(hc.ControlPlaneNamespace, "", "v1", "Service")
					},
				},
				{
					description: "can create Deployments",
					check: func() error {
						return hc.checkCanCreate(hc.ControlPlaneNamespace, "extensions", "v1beta1", "Deployments")
					},
				},
				{
					description: "can create ConfigMaps",
					check: func() error {
						return hc.checkCanCreate(hc.ControlPlaneNamespace, "", "v1", "ConfigMap")
					},
				},
			},
		},
		{
			id: LinkerdControlPlaneExistenceChecks,
			checkers: []checker{
				{
					description: "control plane namespace exists",
					fatal:       true,
					check: func() error {
						return hc.checkNamespace(hc.ControlPlaneNamespace, true)
					},
				},
				{
					description:   "controller pod is running",
					retryDeadline: hc.RetryDeadline,
					fatal:         true,
					check: func() error {
						var err error
						hc.controlPlanePods, err = hc.kubeAPI.GetPodsByNamespace(hc.httpClient, hc.ControlPlaneNamespace)
						if err != nil {
							return err
						}
						return checkControllerRunning(hc.controlPlanePods)
					},
				},
				{
					description: "can initialize the client",
					fatal:       true,
					check: func() (err error) {
						if hc.APIAddr != "" {
							hc.apiClient, err = public.NewInternalClient(hc.ControlPlaneNamespace, hc.APIAddr)
						} else {
							hc.apiClient, err = public.NewExternalClient(hc.ControlPlaneNamespace, hc.kubeAPI)
						}
						return
					},
				},
				{
					description:   "can query the control plane API",
					retryDeadline: hc.RetryDeadline,
					fatal:         true,
					check: func() error {
						_, err := version.GetServerVersion(hc.apiClient)
						return err
					},
				},
			},
		},
		{
			id: LinkerdAPIChecks,
			checkers: []checker{
				{
					description:   "control plane pods are ready",
					retryDeadline: hc.RetryDeadline,
					fatal:         true,
					check: func() error {
						var err error
						hc.controlPlanePods, err = hc.kubeAPI.GetPodsByNamespace(hc.httpClient, hc.ControlPlaneNamespace)
						if err != nil {
							return err
						}
						return validateControlPlanePods(hc.controlPlanePods)
					},
				},
				{
					description:   "can query the control plane API",
					fatal:         true,
					retryDeadline: hc.RetryDeadline,
					checkRPC: func() (*healthcheckPb.SelfCheckResponse, error) {
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						return hc.apiClient.SelfCheck(ctx, &healthcheckPb.SelfCheckRequest{})
					},
				},
			},
		},
		{
			id: LinkerdServiceProfileChecks,
			checkers: []checker{
				{
					description: "no invalid service profiles",
					warning:     true,
					check: func() error {
						return hc.validateServiceProfiles()
					},
				},
			},
		},
		{
			id: LinkerdVersionChecks,
			checkers: []checker{
				{
					description: "can determine the latest version",
					fatal:       true,
					check: func() (err error) {
						if hc.VersionOverride != "" {
							hc.latestVersion = hc.VersionOverride
						} else {
							// The UUID is only known to the web process. At some point we may want
							// to consider providing it in the Public API.
							uuid := "unknown"
							for _, pod := range hc.controlPlanePods {
								if strings.Split(pod.Name, "-")[0] == "web" {
									for _, container := range pod.Spec.Containers {
										if container.Name == "web" {
											for _, arg := range container.Args {
												if strings.HasPrefix(arg, "-uuid=") {
													uuid = strings.TrimPrefix(arg, "-uuid=")
												}
											}
										}
									}
								}
							}
							hc.latestVersion, err = version.GetLatestVersion(uuid, "cli")
						}
						return
					},
				},
				{
					description: "cli is up-to-date",
					warning:     true,
					check: func() error {
						return version.CheckClientVersion(hc.latestVersion)
					},
				},
			},
		},
		{
			id: LinkerdControlPlaneVersionChecks,
			checkers: []checker{
				{
					description: "control plane is up-to-date",
					warning:     true,
					check: func() error {
						return version.CheckServerVersion(hc.apiClient, hc.latestVersion)
					},
				},
			},
		},
		{
			id: LinkerdDataPlaneChecks,
			checkers: []checker{
				{
					description: "data plane namespace exists",
					fatal:       true,
					check: func() error {
						return hc.checkNamespace(hc.DataPlaneNamespace, true)
					},
				},
				{
					description:   "data plane proxies are ready",
					retryDeadline: hc.RetryDeadline,
					fatal:         true,
					check: func() error {
						pods, err := hc.getDataPlanePods()
						if err != nil {
							return err
						}

						return validateDataPlanePods(pods, hc.DataPlaneNamespace)
					},
				},
				{
					description:   "data plane proxy metrics are present in Prometheus",
					retryDeadline: hc.RetryDeadline,
					check: func() error {
						pods, err := hc.getDataPlanePods()
						if err != nil {
							return err
						}

						return validateDataPlanePodReporting(pods)
					},
				},
				{
					description: "data plane is up-to-date",
					warning:     true,
					check: func() error {
						pods, err := hc.getDataPlanePods()
						if err != nil {
							return err
						}

						for _, pod := range pods {
							if pod.ProxyVersion != hc.latestVersion {
								return fmt.Errorf("%s is running version %s but the latest version is %s",
									pod.Name, pod.ProxyVersion, hc.latestVersion)
							}
						}
						return nil
					},
				},
			},
		},
	}
}

// Add adds an arbitrary checker. This should only be used for testing. For
// production code, pass in the desired set of checks when calling
// NewHeathChecker.
func (hc *HealthChecker) Add(categoryID CategoryID, description string, hintURL string, check func() error) {
	hc.addCategory(
		category{
			id: categoryID,
			checkers: []checker{
				checker{
					description: description,
					check:       check,
					hintURL:     hintURL,
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

// RunChecks runs all configured checkers, and passes the results of each
// check to the observer. If a check fails and is marked as fatal, then all
// remaining checks are skipped. If at least one check fails, RunChecks returns
// false; if all checks passed, RunChecks returns true.  Checks which are
// designated as warnings will not cause RunCheck to return false, however.
func (hc *HealthChecker) RunChecks(observer checkObserver) bool {
	success := true

	for _, c := range hc.categories {
		if c.enabled {
			for _, checker := range c.checkers {
				if checker.check != nil {
					if !hc.runCheck(c.id, &checker, observer) {
						if !checker.warning {
							success = false
						}
						if checker.fatal {
							return success
						}
					}
				}

				if checker.checkRPC != nil {
					if !hc.runCheckRPC(c.id, &checker, observer) {
						if !checker.warning {
							success = false
						}
						if checker.fatal {
							return success
						}
					}
				}
			}
		}
	}

	return success
}

func (hc *HealthChecker) runCheck(categoryID CategoryID, c *checker, observer checkObserver) bool {
	for {
		err := c.check()
		checkResult := &CheckResult{
			Category:    categoryID,
			Description: c.description,
			HintURL:     c.hintURL,
			Warning:     c.warning,
			Err:         err,
		}

		if err != nil && time.Now().Before(c.retryDeadline) {
			checkResult.Retry = true
			observer(checkResult)
			time.Sleep(retryWindow)
			continue
		}

		observer(checkResult)
		return err == nil
	}
}

func (hc *HealthChecker) runCheckRPC(categoryID CategoryID, c *checker, observer checkObserver) bool {
	checkRsp, err := c.checkRPC()
	observer(&CheckResult{
		Category:    categoryID,
		Description: c.description,
		Warning:     c.warning,
		Err:         err,
	})
	if err != nil {
		return false
	}

	for _, check := range checkRsp.Results {
		var err error
		if check.Status != healthcheckPb.CheckStatus_OK {
			err = fmt.Errorf(check.FriendlyMessageToUser)
		}
		observer(&CheckResult{
			Category:    categoryID,
			Description: fmt.Sprintf("[%s] %s", check.SubsystemName, check.CheckDescription),
			HintURL:     c.hintURL,
			Warning:     c.warning,
			Err:         err,
		})
		if err != nil {
			return false
		}
	}

	return true
}

// PublicAPIClient returns a fully configured public API client. This client is
// only configured if the KubernetesAPIChecks and LinkerdAPIChecks are
// configured and run first.
func (hc *HealthChecker) PublicAPIClient() pb.ApiClient {
	return hc.apiClient
}

func (hc *HealthChecker) checkNamespace(namespace string, shouldExist bool) error {
	exists, err := hc.kubeAPI.NamespaceExists(hc.httpClient, namespace)
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

func (hc *HealthChecker) getDataPlanePods() ([]*pb.Pod, error) {
	req := &pb.ListPodsRequest{}
	if hc.DataPlaneNamespace != "" {
		req.Selector = &pb.ResourceSelection{
			Resource: &pb.Resource{
				Namespace: hc.DataPlaneNamespace,
			},
		}
	}

	resp, err := hc.apiClient.ListPods(context.Background(), req)
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

func (hc *HealthChecker) checkCanCreate(namespace, group, version, resource string) error {
	if hc.clientset == nil {
		var err error
		hc.clientset, err = kubernetes.NewForConfig(hc.kubeAPI.Config)
		if err != nil {
			return err
		}
	}

	auth := hc.clientset.AuthorizationV1beta1()

	sar := &authorizationapi.SelfSubjectAccessReview{
		Spec: authorizationapi.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Namespace: namespace,
				Verb:      "create",
				Group:     group,
				Version:   version,
				Resource:  resource,
			},
		},
	}

	response, err := auth.SelfSubjectAccessReviews().Create(sar)
	if err != nil {
		return err
	}

	if !response.Status.Allowed {
		if len(response.Status.Reason) > 0 {
			return fmt.Errorf("Missing permissions to create %s: %v", resource, response.Status.Reason)
		}
		return fmt.Errorf("Missing permissions to create %s", resource)
	}
	return nil
}

func (hc *HealthChecker) validateServiceProfiles() error {
	if hc.clientset == nil {
		var err error
		hc.clientset, err = kubernetes.NewForConfig(hc.kubeAPI.Config)
		if err != nil {
			return err
		}
	}

	if hc.spClientset == nil {
		var err error
		hc.spClientset, err = spclient.NewForConfig(hc.kubeAPI.Config)
		if err != nil {
			return err
		}
	}

	svcProfiles, err := hc.spClientset.LinkerdV1alpha1().ServiceProfiles(hc.ControlPlaneNamespace).List(meta_v1.ListOptions{})
	if err != nil {
		return err
	}

	for _, p := range svcProfiles.Items {
		nameParts := strings.Split(p.Name, ".")
		if len(nameParts) != 2+len(clusterZoneSuffix) {
			return fmt.Errorf("ServiceProfile \"%s\" has invalid name (must be \"<service>.<namespace>.svc.cluster.local\")", p.Name)
		}
		for i, part := range nameParts[2:] {
			if part != clusterZoneSuffix[i] {
				return fmt.Errorf("ServiceProfile \"%s\" has invalid name (must be \"<service>.<namespace>.svc.cluster.local\")", p.Name)
			}
		}
		service := nameParts[0]
		namespace := nameParts[1]
		_, err := hc.clientset.Core().Services(namespace).Get(service, meta_v1.GetOptions{})
		if err != nil {
			return fmt.Errorf("ServiceProfile \"%s\" has unknown service: %s", p.Name, err)
		}
		for _, route := range p.Spec.Routes {
			if route.Name == "" {
				return fmt.Errorf("ServiceProfile \"%s\" has a route with no name", p.Name)
			}
			if route.Condition == nil {
				return fmt.Errorf("ServiceProfile \"%s\" has a route with no condition", p.Name)
			}
			err = profiles.ValidateRequestMatch(route.Condition)
			if err != nil {
				return fmt.Errorf("ServiceProfile \"%s\" has a route with an invalid condition: %s", p.Name, err)
			}
			for _, rc := range route.ResponseClasses {
				if rc.Condition == nil {
					return fmt.Errorf("ServiceProfile \"%s\" has a response class with no condition", p.Name)
				}
				err = profiles.ValidateResponseMatch(rc.Condition)
				if err != nil {
					return fmt.Errorf("ServiceProfile \"%s\" has a response class with an invalid condition: %s", p.Name, err)
				}
			}
		}
	}
	return nil
}

func getPodStatuses(pods []v1.Pod) map[string][]v1.ContainerStatus {
	statuses := make(map[string][]v1.ContainerStatus)

	for _, pod := range pods {
		if pod.Status.Phase == v1.PodRunning && strings.HasPrefix(pod.Name, "linkerd-") {
			parts := strings.Split(pod.Name, "-")
			// All control plane pods should have a name that results in at least 4
			// substrings when string.Split on '-'
			if len(parts) >= 4 {
				name := strings.Join(parts[1:len(parts)-2], "-")
				if _, found := statuses[name]; !found {
					statuses[name] = make([]v1.ContainerStatus, 0)
				}
				statuses[name] = append(statuses[name], pod.Status.ContainerStatuses...)
			}
		}
	}

	return statuses
}

func validateControlPlanePods(pods []v1.Pod) error {
	statuses := getPodStatuses(pods)

	names := []string{"controller", "prometheus", "web", "grafana"}
	if _, found := statuses["ca"]; found {
		names = append(names, "ca")
	}
	if _, found := statuses["proxy-injector"]; found {
		names = append(names, "proxy-injector")
	}

	for _, name := range names {
		containers, found := statuses[name]
		if !found {
			return fmt.Errorf("No running pods for \"linkerd-%s\"", name)
		}
		for _, container := range containers {
			if !container.Ready {
				return fmt.Errorf("The \"linkerd-%s\" pod's \"%s\" container is not ready", name,
					container.Name)
			}
		}
	}

	return nil
}

func checkControllerRunning(pods []v1.Pod) error {
	statuses := getPodStatuses(pods)
	if _, ok := statuses["controller"]; !ok {
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
			return fmt.Errorf("The \"%s\" pod is not running",
				pod.Name)
		}

		if !pod.ProxyReady {
			return fmt.Errorf("The \"%s\" container in the \"%s\" pod is not ready",
				k8s.ProxyContainerName, pod.Name)
		}
	}

	return nil
}

func validateDataPlanePodReporting(pods []*pb.Pod) error {
	notInPrometheus := []string{}

	for _, p := range pods {
		// the `Added` field indicates the pod was found in Prometheus
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
