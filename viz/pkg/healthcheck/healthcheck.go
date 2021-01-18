package healthcheck

import (
	"context"
	"crypto/x509"
	"fmt"

	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/viz/metrics-api/client"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/linkerd/linkerd2/viz/pkg"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiregistrationv1client "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/typed/apiregistration/v1"
)

const (
	// LinkerdVizExtensionCheck adds checks related to the Linkerd Viz extension
	LinkerdVizExtensionCheck healthcheck.CategoryID = "linkerd-viz"

	// LinkerdDataPlaneChecks adds data plane checks to validate that the data
	// plane namespace exists, and that the proxy containers are in a ready
	// state and running the latest available version.
	LinkerdDataPlaneChecks healthcheck.CategoryID = "linkerd-data-plane"

	tapTLSSecretName    = "linkerd-tap-k8s-tls"
	tapOldTLSSecretName = "linkerd-tap-tls"

	// linkerdTapAPIServiceName is the name of the tap api service
	// This key is passed to checkApiService method to check whether
	// the api service is available or not
	linkerdTapAPIServiceName = "v1alpha1.tap.linkerd.io"
)

// HealthChecker wraps Linkerd's main healthchecker, adding extra fields for Viz
type HealthChecker struct {
	*healthcheck.HealthChecker
	vizNamespace string
	vizAPIClient pb.ApiClient
}

// NewHealthChecker returns an initialized HealthChecker for Viz
func NewHealthChecker(categoryIDs []healthcheck.CategoryID, options *healthcheck.Options) *HealthChecker {
	parentHC := healthcheck.NewHealthChecker(categoryIDs, options)
	hc := &HealthChecker{HealthChecker: parentHC}
	parentHC.AppendCategories(*hc.vizCategory())
	return hc
}

// VizAPIClient returns a fully configured Viz API client
func (hc *HealthChecker) VizAPIClient() pb.ApiClient {
	return hc.vizAPIClient
}

// RunChecks implements the healthcheck.Runner interface
func (hc *HealthChecker) RunChecks(observer healthcheck.CheckObserver) bool {
	return hc.HealthChecker.RunChecks(observer)
}

func (hc *HealthChecker) vizCategory() *healthcheck.Category {
	checkers := []healthcheck.Checker{}
	checkers = append(checkers,
		*healthcheck.NewChecker("linkerd-viz Namespace exists").
			WithHintAnchor("l5d-viz-ns-exists").
			Fatal().
			WithCheck(func(ctx context.Context) (err error) {
				hc.vizNamespace, err = pkg.GetVizNamespace(ctx, hc.KubeAPIClient())
				return
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("linkerd-viz ClusterRoles exist").
			WithHintAnchor("l5d-viz-cr-exists").
			Fatal().
			Warning().
			WithCheck(func(ctx context.Context) error {
				return healthcheck.CheckClusterRoles(ctx, hc.KubeAPIClient(), true, []string{fmt.Sprintf("linkerd-%s-prometheus", hc.vizNamespace), fmt.Sprintf("linkerd-%s-tap", hc.vizNamespace)}, "")
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("linkerd-viz ClusterRoleBindings exist").
			WithHintAnchor("l5d-viz-crb-exists").
			Fatal().
			Warning().
			WithCheck(func(ctx context.Context) error {
				return healthcheck.CheckClusterRoleBindings(ctx, hc.KubeAPIClient(), true, []string{fmt.Sprintf("linkerd-%s-prometheus", hc.vizNamespace), fmt.Sprintf("linkerd-%s-tap", hc.vizNamespace)}, "")
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("linkerd-viz ConfigMaps exist").
			WithHintAnchor("l5d-viz-cm-exists").
			Fatal().
			Warning().
			WithCheck(func(ctx context.Context) error {
				return healthcheck.CheckConfigMaps(ctx, hc.KubeAPIClient(), hc.vizNamespace, true, []string{"linkerd-prometheus-config", "linkerd-grafana-config"}, "")
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("tap API server has valid cert").
			WithHintAnchor("l5d-tap-cert-valid").
			Fatal().
			WithCheck(func(ctx context.Context) error {
				anchors, err := fetchTapCaBundle(ctx, hc.KubeAPIClient())
				if err != nil {
					return err
				}
				cert, err := hc.FetchCredsFromSecret(ctx, hc.vizNamespace, tapTLSSecretName)
				if kerrors.IsNotFound(err) {
					cert, err = hc.FetchCredsFromOldSecret(ctx, hc.vizNamespace, tapOldTLSSecretName)
				}
				if err != nil {
					return err
				}

				identityName := fmt.Sprintf("linkerd-tap.%s.svc", hc.vizNamespace)
				return hc.CheckCertAndAnchors(cert, anchors, identityName)
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("tap API server cert is valid for at least 60 days").
			WithHintAnchor("l5d-webhook-cert-not-expiring-soon").
			Warning().
			WithCheck(func(ctx context.Context) error {
				cert, err := hc.FetchCredsFromSecret(ctx, hc.vizNamespace, tapTLSSecretName)
				if kerrors.IsNotFound(err) {
					cert, err = hc.FetchCredsFromOldSecret(ctx, hc.vizNamespace, tapOldTLSSecretName)
				}
				if err != nil {
					return err
				}
				return hc.CheckCertAndAnchorsExpiringSoon(cert)
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("tap API service is running").
			WithHintAnchor("l5d-tap-api").
			Warning().
			WithCheck(func(ctx context.Context) error {
				return hc.CheckAPIService(ctx, linkerdTapAPIServiceName)
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("viz extension pods are running").
			WithHintAnchor("l5d-viz-pods-running").
			Warning().
			WithRetryDeadline(hc.RetryDeadline).
			SurfaceErrorOnRetry().
			WithCheck(func(ctx context.Context) error {
				pods, err := hc.KubeAPIClient().GetPodsByNamespace(ctx, hc.vizNamespace)
				if err != nil {
					return err
				}

				// Check for relevant pods to be present
				err = healthcheck.CheckForPods(pods, []string{"linkerd-grafana", "linkerd-prometheus", "linkerd-web", "linkerd-tap"})
				if err != nil {
					return err
				}

				return healthcheck.CheckPodsRunning(pods)
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("linkerd-viz pods are injected").
			WithHintAnchor("l5d-viz-pods-injection").
			Warning().
			WithCheck(func(ctx context.Context) error {
				pods, err := hc.KubeAPIClient().GetPodsByNamespace(ctx, hc.vizNamespace)
				if err != nil {
					return err
				}
				return healthcheck.CheckIfDataPlanePodsExist(pods)
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("can initialize the client").
			WithHintAnchor("l5d-viz-existence-client").
			Fatal().
			WithCheck(func(ctx context.Context) (err error) {
				hc.vizAPIClient, err = client.NewExternalClient(ctx, hc.vizNamespace, hc.KubeAPIClient())
				return
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("viz extension self-check").
			WithHintAnchor("l5d-api-control-api").
			Fatal().
			// to avoid confusing users with a prometheus readiness error, we only show
			// "waiting for check to complete" while things converge. If after the timeout
			// it still hasn't converged, we show the real error (a 503 usually).
			WithRetryDeadline(hc.RetryDeadline).
			WithCheckRPC(func(ctx context.Context) (*healthcheckPb.SelfCheckResponse, error) {
				return hc.vizAPIClient.SelfCheck(ctx, &healthcheckPb.SelfCheckRequest{})
			}))

	return healthcheck.NewCategory(LinkerdVizExtensionCheck, checkers, true)
}

// TODO: run these checks after wiring `linkerd viz check --proxy`
/*func (hc *HealthChecker) vizDataplaneCategory() *healthcheck.Category {
	checkers := []healthcheck.Checker{}
	checkers = append(checkers,
		*healthcheck.NewChecker("data plane namespace exists").
			WithHintAnchor("l5d-data-plane-exists").
			Fatal().
			WithCheck(func(ctx context.Context) error {
				if hc.DataPlaneNamespace == "" {
					// when checking proxies in all namespaces, this check is a no-op
					return nil
				}
				return hc.CheckNamespace(ctx, hc.DataPlaneNamespace, true)
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("data plane proxies are ready").
			WithHintAnchor("l5d-data-plane-ready").
			WithRetryDeadline(hc.RetryDeadline).
			Fatal().
			WithCheck(func(ctx context.Context) error {
				pods, err := hc.GetDataPlanePods(ctx)
				if err != nil {
					return err
				}

				return validateDataPlanePods(pods, hc.DataPlaneNamespace)
			}))
	checkers = append(checkers,
		*healthcheck.NewChecker("data plane is up-to-date").
			WithHintAnchor("l5d-data-plane-version").
			Warning().
			WithCheck(func(ctx context.Context) error {
				pods, err := hc.GetDataPlanePods(ctx)
				if err != nil {
					return err
				}

				outdatedPods := []string{}
				for _, pod := range pods {
					err = hc.LatestVersions().Match(pod.ProxyVersion)
					if err != nil {
						outdatedPods = append(outdatedPods, fmt.Sprintf("\t* %s (%s)", pod.Name, pod.ProxyVersion))
					}
				}
				if len(outdatedPods) > 0 {
					podList := strings.Join(outdatedPods, "\n")
					return fmt.Errorf("Some data plane pods are not running the current version:\n%s", podList)
				}
				return nil
			}))
	checkers = append(checkers,
		*healthcheck.NewChecker("data plane and cli versions match").
			WithHintAnchor("l5d-data-plane-cli-version").
			Warning().
			WithCheck(func(ctx context.Context) error {
				pods, err := hc.GetDataPlanePods(ctx)
				if err != nil {
					return err
				}

				for _, pod := range pods {
					if pod.ProxyVersion != version.Version {
						return fmt.Errorf("%s running %s but cli running %s", pod.Name, pod.ProxyVersion, version.Version)
					}
				}
				return nil
			}))

	return healthcheck.NewCategory(LinkerdDataPlaneChecks, checkers, true)
}

// GetDataPlanePods returns all the pods with data plane
func (hc *HealthChecker) GetDataPlanePods(ctx context.Context) ([]*pb.Pod, error) {
	req := &pb.ListPodsRequest{}
	if hc.DataPlaneNamespace != "" {
		req.Selector = &pb.ResourceSelection{
			Resource: &pb.Resource{
				Namespace: hc.DataPlaneNamespace,
			},
		}
	}

	resp, err := hc.vizAPIClient.ListPods(ctx, req)
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

func validateDataPlanePods(pods []*pb.Pod, targetNamespace string) error {
	if len(pods) == 0 {
		msg := fmt.Sprintf("No \"%s\" containers found", k8s.ProxyContainerName)
		if targetNamespace != "" {
			msg += fmt.Sprintf(" in the \"%s\" namespace", targetNamespace)
		}
		return fmt.Errorf(msg)
	}

	for _, pod := range pods {
		if pod.Status != "Running" && pod.Status != "Evicted" {
			return fmt.Errorf("The \"%s\" pod is not running",
				pod.Name)
		}

		if !pod.ProxyReady {
			return fmt.Errorf("The \"%s\" container in the \"%s\" pod is not ready",
				k8s.ProxyContainerName, pod.Name)
		}
	}

	return nil
}*/

func fetchTapCaBundle(ctx context.Context, kubeAPI *k8s.KubernetesAPI) ([]*x509.Certificate, error) {
	apiServiceClient, err := apiregistrationv1client.NewForConfig(kubeAPI.Config)
	if err != nil {
		return nil, err
	}

	apiService, err := apiServiceClient.APIServices().Get(ctx, linkerdTapAPIServiceName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	caBundle, err := tls.DecodePEMCertificates(string(apiService.Spec.CABundle))
	if err != nil {
		return nil, err
	}
	return caBundle, nil
}
