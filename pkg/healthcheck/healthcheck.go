package healthcheck

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/controller/api/public"
	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	"k8s.io/api/core/v1"
	k8sVersion "k8s.io/apimachinery/pkg/version"
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
	LinkerdPreInstallCategory = "linkerd-ns"
	LinkerdDataPlaneCategory  = "linkerd-data-plane"
	LinkerdAPICategory        = "linkerd-api"
	LinkerdVersionCategory    = "linkerd-version"
)

var (
	maxRetries  = 10
	retryWindow = 5 * time.Second
)

type checker struct {
	category    string
	description string
	fatal       bool
	retry       bool
	check       func() error
	checkRPC    func() (*healthcheckPb.SelfCheckResponse, error)
}

type CheckResult struct {
	Category    string
	Description string
	Retry       bool
	Err         error
}

type checkObserver func(*CheckResult)

type HealthCheckOptions struct {
	ControlPlaneNamespace          string
	DataPlaneNamespace             string
	KubeConfig                     string
	APIAddr                        string
	VersionOverride                string
	ShouldRetry                    bool
	ShouldCheckKubeVersion         bool
	ShouldCheckControlPlaneVersion bool
	ShouldCheckDataPlaneVersion    bool
}

type HealthChecker struct {
	checkers []*checker
	*HealthCheckOptions

	// these fields are set in the process of running checks
	kubeAPI       *k8s.KubernetesAPI
	httpClient    *http.Client
	kubeVersion   *k8sVersion.Info
	apiClient     pb.ApiClient
	latestVersion string
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
			hc.kubeAPI, err = k8s.NewAPI(hc.KubeConfig)
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
			fatal:       false,
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
		fatal:       false,
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
		category:    LinkerdAPICategory,
		description: "control plane pods are ready",
		retry:       hc.ShouldRetry,
		fatal:       true,
		check: func() error {
			pods, err := hc.kubeAPI.GetPodsByNamespace(hc.httpClient, hc.ControlPlaneNamespace)
			if err != nil {
				return err
			}
			return validateControlPlanePods(pods)
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
		category:    LinkerdAPICategory,
		description: "can query the control plane API",
		fatal:       true,
		checkRPC: func() (*healthcheckPb.SelfCheckResponse, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return hc.apiClient.SelfCheck(ctx, &healthcheckPb.SelfCheckRequest{})
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
		category:    LinkerdDataPlaneCategory,
		description: "data plane proxies are ready",
		retry:       hc.ShouldRetry,
		fatal:       true,
		check: func() error {
			pods, err := hc.kubeAPI.GetPodsByControllerNamespace(
				hc.httpClient,
				hc.ControlPlaneNamespace,
				hc.DataPlaneNamespace,
			)
			if err != nil {
				return err
			}
			return validateDataPlanePods(pods, hc.DataPlaneNamespace)
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
				hc.latestVersion, err = version.GetLatestVersion()
			}
			return
		},
	})

	hc.checkers = append(hc.checkers, &checker{
		category:    LinkerdVersionCategory,
		description: "cli is up-to-date",
		fatal:       false,
		check: func() error {
			return version.CheckClientVersion(hc.latestVersion)
		},
	})

	if hc.ShouldCheckControlPlaneVersion {
		hc.checkers = append(hc.checkers, &checker{
			category:    LinkerdVersionCategory,
			description: "control plane is up-to-date",
			fatal:       false,
			check: func() error {
				return version.CheckServerVersion(hc.apiClient, hc.latestVersion)
			},
		})
	}

	if hc.ShouldCheckDataPlaneVersion {
		hc.checkers = append(hc.checkers, &checker{
			category:    LinkerdVersionCategory,
			description: "data plane is up-to-date",
			fatal:       false,
			check: func() error {
				pods, err := hc.kubeAPI.GetPodsByControllerNamespace(
					hc.httpClient,
					hc.ControlPlaneNamespace,
					hc.DataPlaneNamespace,
				)
				if err != nil {
					return err
				}
				return hc.kubeAPI.CheckProxyVersion(pods, hc.latestVersion)
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
// false; if all checks passed, RunChecks returns true.
func (hc *HealthChecker) RunChecks(observer checkObserver) bool {
	success := true

	for _, checker := range hc.checkers {
		if checker.check != nil {
			if !hc.runCheck(checker, observer) {
				success = false
				if checker.fatal {
					break
				}
			}
		}

		if checker.checkRPC != nil {
			if !hc.runCheckRPC(checker, observer) {
				success = false
				if checker.fatal {
					break
				}
			}
		}
	}

	return success
}

func (hc *HealthChecker) runCheck(c *checker, observer checkObserver) bool {
	var retries int
	if c.retry {
		retries = maxRetries
	}

	for {
		err := c.check()
		checkResult := &CheckResult{
			Category:    c.category,
			Description: c.description,
			Err:         err,
		}

		if err != nil && retries > 0 {
			retries--
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

	names := []string{"controller", "grafana", "prometheus", "web"}
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

func validateDataPlanePods(pods []v1.Pod, targetNamespace string) error {
	if len(pods) == 0 {
		msg := fmt.Sprintf("No \"%s\" containers found", k8s.ProxyContainerName)
		if targetNamespace != "" {
			msg += fmt.Sprintf(" in the \"%s\" namespace", targetNamespace)
		}
		return fmt.Errorf(msg)
	}

	for _, pod := range pods {
		if pod.Status.Phase != v1.PodRunning {
			return fmt.Errorf("The \"%s\" pod in the \"%s\" namespace is not running",
				pod.Name, pod.Namespace)
		}

		var proxyReady bool
		for _, container := range pod.Status.ContainerStatuses {
			if container.Name == k8s.ProxyContainerName {
				proxyReady = container.Ready
			}
		}

		if !proxyReady {
			return fmt.Errorf("The \"%s\" container in the \"%s\" pod in the \"%s\" namespace is not ready",
				k8s.ProxyContainerName, pod.Name, pod.Namespace)
		}
	}

	return nil
}
