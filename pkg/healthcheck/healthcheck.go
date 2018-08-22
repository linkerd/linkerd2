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
	// of pre-install checks. This check is dependent on the output of
	// KubernetesAPIChecks, so those checks must be added first.
	LinkerdPreInstallChecks

	// LinkerdAPIChecks adds a series of checks to validate that the control plane
	// namespace exists and that it's successfully serving the public API. These
	// checks are dependent on the output of KubernetesAPIChecks, so those checks
	// must be added first.
	LinkerdAPIChecks

	// LinkerdVersionChecks adds a series of checks to validate that the CLI and
	// control plane are running the latest available version. These checks are
	// dependent on the output of AddLinkerdAPIChecks, so those checks must be
	// added first, unless the ShouldCheckControllerVersion option is false.
	LinkerdVersionChecks

	KubernetesAPICategory     = "kubernetes-api"
	LinkerdPreInstallCategory = "linkerd-ns"
	LinkerdAPICategory        = "linkerd-api"
	LinkerdVersionCategory    = "linkerd-version"
)

type checker struct {
	category    string
	description string
	fatal       bool
	check       func() error
	checkRPC    func() (*healthcheckPb.SelfCheckResponse, error)
}

type CheckResult struct {
	Category    string
	Description string
	Err         error
}

type checkObserver func(*CheckResult)

type HealthCheckOptions struct {
	Namespace                    string
	KubeConfig                   string
	APIAddr                      string
	VersionOverride              string
	ShouldCheckKubeVersion       bool
	ShouldCheckControllerVersion bool
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
			exists, err := hc.kubeAPI.NamespaceExists(hc.httpClient, hc.Namespace)
			if err != nil {
				return err
			}
			if exists {
				return fmt.Errorf("The \"%s\" namespace already exists", hc.Namespace)
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
			exists, err := hc.kubeAPI.NamespaceExists(hc.httpClient, hc.Namespace)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("The \"%s\" namespace does not exist", hc.Namespace)
			}
			return nil
		},
	})

	hc.checkers = append(hc.checkers, &checker{
		category:    LinkerdAPICategory,
		description: "control plane pods are ready",
		fatal:       true,
		check: func() error {
			pods, err := hc.kubeAPI.GetPodsForNamespace(hc.httpClient, hc.Namespace)
			if err != nil {
				return err
			}
			return validatePods(pods)
		},
	})

	hc.checkers = append(hc.checkers, &checker{
		category:    LinkerdAPICategory,
		description: "can initialize the client",
		fatal:       true,
		check: func() (err error) {
			if hc.APIAddr != "" {
				hc.apiClient, err = public.NewInternalClient(hc.Namespace, hc.APIAddr)
			} else {
				hc.apiClient, err = public.NewExternalClient(hc.Namespace, hc.kubeAPI)
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

	if hc.ShouldCheckControllerVersion {
		hc.checkers = append(hc.checkers, &checker{
			category:    LinkerdVersionCategory,
			description: "control plane is up-to-date",
			fatal:       false,
			check: func() error {
				return version.CheckServerVersion(hc.apiClient, hc.latestVersion)
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
	err := c.check()
	observer(&CheckResult{
		Category:    c.category,
		Description: c.description,
		Err:         err,
	})
	return err == nil
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

func validatePods(pods []v1.Pod) error {
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
			return fmt.Errorf("No running pods for %s", name)
		}
		for _, container := range containers {
			if !container.Ready {
				return fmt.Errorf("The %s pod's %s container is not ready", name,
					container.Name)
			}
		}
	}

	return nil
}
