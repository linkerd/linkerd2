package healthcheck

import (
	"context"
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

type Checks int

const (
	// KubernetesAPIChecks adds a series of checks to validate that the caller is
	// configured to interact with a working Kubernetes cluster and that the
	// cluster meets the minimum version requirements, unless the
	// ShouldCheckKubeVersion option is false.
	KubernetesAPIChecks Checks = iota

	// LinkerdPreInstallChecks adds a check to validate that the control plane
	// namespace does not already exist. This check only runs as part of the set
	// of pre-install checks.
	// This check is dependent on the output of KubernetesAPIChecks, so those
	// checks must be added first.
	LinkerdPreInstallChecks

	// LinkerdDataPlaneChecks adds a data plane check to validate that the proxy
	// containers are in the ready state.
	// This check is dependent on the output of KubernetesAPIChecks, so those
	// checks must be added first.
	LinkerdDataPlaneChecks

	// LinkerdAPIChecks adds a series of checks to validate that the control plane
	// namespace exists and that it's successfully serving the public API.
	// These checks are dependent on the output of KubernetesAPIChecks, so those
	// checks must be added first.
	LinkerdAPIChecks

	// LinkerdVersionChecks adds a series of checks to validate that the CLI,
	// control plane, and data plane are running the latest available version.
	// These checks are dependent on the output of AddLinkerdAPIChecks, so those
	// checks must be added first, unless the the ShouldCheckControlPlaneVersion
	// and ShouldCheckDataPlaneVersion options are false.
	LinkerdVersionChecks

	KubernetesAPICategory     = "kubernetes-api"
	LinkerdPreInstallCategory = "kubernetes-setup"
	LinkerdDataPlaneCategory  = "linkerd-data-plane"
	LinkerdAPICategory        = "linkerd-api"
	LinkerdVersionCategory    = "linkerd-version"
)

var (
	maxRetries        = 60
	retryWindow       = 5 * time.Second
	clusterZoneSuffix = []string{"svc", "cluster", "local"}
)

type checker struct {
	// category is one of the *Category constants defined above
	category string

	// description is the short description that's printed to the command line
	// when the check is executed
	description string

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

type CheckResult struct {
	Category    string
	Description string
	Retry       bool
	Warning     bool
	Err         error
}

type checkObserver func(*CheckResult)

type HealthCheckOptions struct {
	ControlPlaneNamespace          string
	DataPlaneNamespace             string
	KubeConfig                     string
	KubeContext                    string
	APIAddr                        string
	VersionOverride                string
	RetryDeadline                  time.Time
	ShouldCheckKubeVersion         bool
	ShouldCheckControlPlaneVersion bool
	ShouldCheckDataPlaneVersion    bool
	SingleNamespace                bool
}

type HealthChecker struct {
	checkers []*checker
	*HealthCheckOptions

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

func NewHealthChecker(checks []Checks, options *HealthCheckOptions) *HealthChecker {
	hc := &HealthChecker{
		checkers:           make([]*checker, 0),
		HealthCheckOptions: options,
	}

	for _, check := range checks {
		switch check {
		case KubernetesAPIChecks:
			hc.addKubernetesAPIChecks()
		case LinkerdPreInstallChecks:
			hc.addLinkerdPreInstallChecks()
		case LinkerdDataPlaneChecks:
			hc.addLinkerdDataPlaneChecks()
		case LinkerdAPIChecks:
			hc.addLinkerdAPIChecks()
		case LinkerdVersionChecks:
			hc.addLinkerdVersionChecks()
		}
	}

	return hc
}

func (hc *HealthChecker) addKubernetesAPIChecks() {
	hc.checkers = append(hc.checkers, &checker{
		category:    KubernetesAPICategory,
		description: "can initialize the client",
		fatal:       true,
		check: func() (err error) {
			hc.kubeAPI, err = k8s.NewAPI(hc.KubeConfig, hc.KubeContext)
			return
		},
	})

	hc.checkers = append(hc.checkers, &checker{
		category:    KubernetesAPICategory,
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
	})

	if hc.ShouldCheckKubeVersion {
		hc.checkers = append(hc.checkers, &checker{
			category:    KubernetesAPICategory,
			description: "is running the minimum Kubernetes API version",
			check: func() error {
				return hc.kubeAPI.CheckVersion(hc.kubeVersion)
			},
		})
	}
}

func (hc *HealthChecker) addLinkerdPreInstallChecks() {
	hc.checkers = append(hc.checkers, &checker{
		category:    LinkerdPreInstallCategory,
		description: "control plane namespace does not already exist",
		check: func() error {
			exists, err := hc.kubeAPI.NamespaceExists(hc.httpClient, hc.ControlPlaneNamespace)
			if err != nil {
				return err
			}
			if exists {
				return fmt.Errorf("The \"%s\" namespace already exists", hc.ControlPlaneNamespace)
			}
			return nil
		},
	})

	hc.checkers = append(hc.checkers, &checker{
		category:    LinkerdPreInstallCategory,
		description: "can create Namespaces",
		check: func() error {
			return hc.checkCanCreate("", "", "v1", "Namespace")
		},
	})

	roleType := "ClusterRole"
	roleBindingType := "ClusterRoleBinding"
	if hc.SingleNamespace {
		roleType = "Role"
		roleBindingType = "RoleBinding"
	}

	hc.checkers = append(hc.checkers, &checker{
		category:    LinkerdPreInstallCategory,
		description: fmt.Sprintf("can create %ss", roleType),
		check: func() error {
			return hc.checkCanCreate("", "rbac.authorization.k8s.io", "v1beta1", roleType)
		},
	})

	hc.checkers = append(hc.checkers, &checker{
		category:    LinkerdPreInstallCategory,
		description: fmt.Sprintf("can create %ss", roleBindingType),
		check: func() error {
			return hc.checkCanCreate("", "rbac.authorization.k8s.io", "v1beta1", roleBindingType)
		},
	})

	hc.checkers = append(hc.checkers, &checker{
		category:    LinkerdPreInstallCategory,
		description: "can create ServiceAccounts",
		check: func() error {
			return hc.checkCanCreate(hc.ControlPlaneNamespace, "", "v1", "ServiceAccount")
		},
	})

	hc.checkers = append(hc.checkers, &checker{
		category:    LinkerdPreInstallCategory,
		description: "can create Services",
		check: func() error {
			return hc.checkCanCreate(hc.ControlPlaneNamespace, "", "v1", "Service")
		},
	})

	hc.checkers = append(hc.checkers, &checker{
		category:    LinkerdPreInstallCategory,
		description: "can create Deployments",
		check: func() error {
			return hc.checkCanCreate(hc.ControlPlaneNamespace, "extensions", "v1beta1", "Deployments")
		},
	})

	hc.checkers = append(hc.checkers, &checker{
		category:    LinkerdPreInstallCategory,
		description: "can create ConfigMaps",
		check: func() error {
			return hc.checkCanCreate(hc.ControlPlaneNamespace, "", "v1", "ConfigMap")
		},
	})

	hc.checkers = append(hc.checkers, &checker{
		category:    LinkerdPreInstallCategory,
		description: "can create CustomResourceDefinitions",
		check: func() error {
			return hc.checkCanCreate(hc.ControlPlaneNamespace, "apiextensions.k8s.io", "v1beta1", "CustomResourceDefinition")
		},
	})
}

func (hc *HealthChecker) addLinkerdAPIChecks() {
	hc.checkers = append(hc.checkers, &checker{
		category:    LinkerdAPICategory,
		description: "control plane namespace exists",
		fatal:       true,
		check: func() error {
			return hc.checkNamespace(hc.ControlPlaneNamespace)
		},
	})

	hc.checkers = append(hc.checkers, &checker{
		category:      LinkerdAPICategory,
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
	})

	hc.checkers = append(hc.checkers, &checker{
		category:    LinkerdAPICategory,
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
	})

	hc.checkers = append(hc.checkers, &checker{
		category:      LinkerdAPICategory,
		description:   "can query the control plane API",
		fatal:         true,
		retryDeadline: hc.RetryDeadline,
		checkRPC: func() (*healthcheckPb.SelfCheckResponse, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return hc.apiClient.SelfCheck(ctx, &healthcheckPb.SelfCheckRequest{})
		},
	})

	hc.checkers = append(hc.checkers, &checker{
		category:    LinkerdAPICategory,
		description: "no invalid service profiles",
		warning:     true,
		check: func() error {
			return hc.validateServiceProfiles()
		},
	})
}

func (hc *HealthChecker) addLinkerdDataPlaneChecks() {
	if hc.DataPlaneNamespace != "" {
		hc.checkers = append(hc.checkers, &checker{
			category:    LinkerdDataPlaneCategory,
			description: "data plane namespace exists",
			fatal:       true,
			check: func() error {
				return hc.checkNamespace(hc.DataPlaneNamespace)
			},
		})
	}

	hc.checkers = append(hc.checkers, &checker{
		category:      LinkerdDataPlaneCategory,
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
	})

	hc.checkers = append(hc.checkers, &checker{
		category:      LinkerdDataPlaneCategory,
		description:   "data plane proxy metrics are present in Prometheus",
		retryDeadline: hc.RetryDeadline,
		check: func() error {
			pods, err := hc.getDataPlanePods()
			if err != nil {
				return err
			}

			return validateDataPlanePodReporting(pods)
		},
	})
}

func (hc *HealthChecker) addLinkerdVersionChecks() {
	hc.checkers = append(hc.checkers, &checker{
		category:    LinkerdVersionCategory,
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
	})

	hc.checkers = append(hc.checkers, &checker{
		category:    LinkerdVersionCategory,
		description: "cli is up-to-date",
		warning:     true,
		check: func() error {
			return version.CheckClientVersion(hc.latestVersion)
		},
	})

	if hc.ShouldCheckControlPlaneVersion {
		hc.checkers = append(hc.checkers, &checker{
			category:    LinkerdVersionCategory,
			description: "control plane is up-to-date",
			warning:     true,
			check: func() error {
				return version.CheckServerVersion(hc.apiClient, hc.latestVersion)
			},
		})
	}

	if hc.ShouldCheckDataPlaneVersion {
		hc.checkers = append(hc.checkers, &checker{
			category:    LinkerdVersionCategory,
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
		})
	}
}

// Add adds an arbitrary checker. This should only be used for testing. For
// production code, pass in the desired set of checks when calling
// NewHeathChecker.
func (hc *HealthChecker) Add(category, description string, check func() error) {
	hc.checkers = append(hc.checkers, &checker{
		category:    category,
		description: description,
		check:       check,
	})
}

// RunChecks runs all configured checkers, and passes the results of each
// check to the observer. If a check fails and is marked as fatal, then all
// remaining checks are skipped. If at least one check fails, RunChecks returns
// false; if all checks passed, RunChecks returns true.  Checks which are
// designated as warnings will not cause RunCheck to return false, however.
func (hc *HealthChecker) RunChecks(observer checkObserver) bool {
	success := true

	for _, checker := range hc.checkers {
		if checker.check != nil {
			if !hc.runCheck(checker, observer) {
				if !checker.warning {
					success = false
				}
				if checker.fatal {
					break
				}
			}
		}

		if checker.checkRPC != nil {
			if !hc.runCheckRPC(checker, observer) {
				if !checker.warning {
					success = false
				}
				if checker.fatal {
					break
				}
			}
		}
	}

	return success
}

func (hc *HealthChecker) runCheck(c *checker, observer checkObserver) bool {
	for {
		err := c.check()
		checkResult := &CheckResult{
			Category:    c.category,
			Description: c.description,
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

func (hc *HealthChecker) runCheckRPC(c *checker, observer checkObserver) bool {
	checkRsp, err := c.checkRPC()
	observer(&CheckResult{
		Category:    c.category,
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
			Category:    fmt.Sprintf("%s[%s]", c.category, check.SubsystemName),
			Description: check.CheckDescription,
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

func (hc *HealthChecker) checkNamespace(namespace string) error {
	exists, err := hc.kubeAPI.NamespaceExists(hc.httpClient, namespace)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("The \"%s\" namespace does not exist", namespace)
	}
	return nil
}

func (hc *HealthChecker) getDataPlanePods() ([]*pb.Pod, error) {
	req := &pb.ListPodsRequest{}
	if hc.DataPlaneNamespace != "" {
		req.Namespace = hc.DataPlaneNamespace
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

func validateControlPlanePods(pods []v1.Pod) error {
	statuses := make(map[string][]v1.ContainerStatus)

	for _, pod := range pods {
		if pod.Status.Phase == v1.PodRunning {
			name := strings.Split(pod.Name, "-")[0]
			if _, found := statuses[name]; !found {
				statuses[name] = make([]v1.ContainerStatus, 0)
			}
			statuses[name] = append(statuses[name], pod.Status.ContainerStatuses...)
		}
	}

	names := []string{"controller", "prometheus", "web", "grafana"}
	if _, found := statuses["ca"]; found {
		names = append(names, "ca")
	}

	for _, name := range names {
		containers, found := statuses[name]
		if !found {
			return fmt.Errorf("No running pods for \"%s\"", name)
		}
		for _, container := range containers {
			if !container.Ready {
				return fmt.Errorf("The \"%s\" pod's \"%s\" container is not ready", name,
					container.Name)
			}
		}
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
