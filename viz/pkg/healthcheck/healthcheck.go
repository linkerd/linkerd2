package healthcheck

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/linkerd/linkerd2/viz/metrics-api/client"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/linkerd/linkerd2/viz/pkg/labels"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiregistrationv1client "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/typed/apiregistration/v1"
)

const (
	// VizExtensionName is the name of the viz extension
	VizExtensionName = "viz"

	// LinkerdVizExtensionCheck adds checks related to the Linkerd Viz extension
	LinkerdVizExtensionCheck healthcheck.CategoryID = "linkerd-viz"

	// LinkerdVizExtensionDataPlaneCheck adds checks related to dataplane for the linkerd-viz extension
	LinkerdVizExtensionDataPlaneCheck healthcheck.CategoryID = "linkerd-viz-data-plane"

	tapTLSSecretName    = "tap-k8s-tls"
	tapOldTLSSecretName = "linkerd-tap-tls"

	// linkerdTapAPIServiceName is the name of the tap api service
	// This key is passed to checkApiService method to check whether
	// the api service is available or not
	linkerdTapAPIServiceName = "v1alpha1.tap.linkerd.io"
)

// HealthChecker wraps Linkerd's main healthchecker, adding extra fields for Viz
type HealthChecker struct {
	*healthcheck.HealthChecker
	vizAPIClient          pb.ApiClient
	vizNamespace          string
	externalPrometheusURL string
}

// NewHealthChecker returns an initialized HealthChecker for Viz
// The parentCheckIDs are the category IDs of the linkerd core checks that
// are to be ran together with this instance
// The returned instance does not contain any of the viz Categories and
// to be explicitly added by using hc.AppendCategories
func NewHealthChecker(parentCheckIDs []healthcheck.CategoryID, options *healthcheck.Options) *HealthChecker {
	parentHC := healthcheck.NewHealthChecker(parentCheckIDs, options)
	return &HealthChecker{HealthChecker: parentHC}
}

// VizAPIClient returns a fully configured Viz API client
func (hc *HealthChecker) VizAPIClient() pb.ApiClient {
	return hc.vizAPIClient
}

// RunChecks implements the healthcheck.Runner interface
func (hc *HealthChecker) RunChecks(observer healthcheck.CheckObserver) bool {
	return hc.HealthChecker.RunChecks(observer)
}

// VizCategory returns a healthcheck.Category containing checkers
// to verify the health of viz components
func (hc *HealthChecker) VizCategory() *healthcheck.Category {

	return healthcheck.NewCategory(LinkerdVizExtensionCheck, []healthcheck.Checker{
		*healthcheck.NewChecker("linkerd-viz Namespace exists").
			WithHintAnchor("l5d-viz-ns-exists").
			Fatal().
			WithCheck(func(ctx context.Context) error {
				vizNs, err := hc.KubeAPIClient().GetNamespaceWithExtensionLabel(ctx, "viz")
				if err != nil {
					return err
				}

				hc.vizNamespace = vizNs.Name
				hc.externalPrometheusURL = vizNs.Annotations[labels.VizExternalPrometheus]
				return nil
			}),
		*healthcheck.NewChecker("linkerd-viz ClusterRoles exist").
			WithHintAnchor("l5d-viz-cr-exists").
			Fatal().
			Warning().
			WithCheck(func(ctx context.Context) error {
				return healthcheck.CheckClusterRoles(ctx, hc.KubeAPIClient(), true, []string{fmt.Sprintf("linkerd-%s-tap", hc.vizNamespace), fmt.Sprintf("linkerd-%s-metrics-api", hc.vizNamespace), fmt.Sprintf("linkerd-%s-tap-admin", hc.vizNamespace), "linkerd-tap-injector"}, "")
			}),
		*healthcheck.NewChecker("linkerd-viz ClusterRoleBindings exist").
			WithHintAnchor("l5d-viz-crb-exists").
			Fatal().
			Warning().
			WithCheck(func(ctx context.Context) error {
				return healthcheck.CheckClusterRoleBindings(ctx, hc.KubeAPIClient(), true, []string{fmt.Sprintf("linkerd-%s-tap", hc.vizNamespace), fmt.Sprintf("linkerd-%s-metrics-api", hc.vizNamespace), fmt.Sprintf("linkerd-%s-tap-auth-delegator", hc.vizNamespace), "linkerd-tap-injector"}, "")
			}),
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

				identityName := fmt.Sprintf("tap.%s.svc", hc.vizNamespace)
				return hc.CheckCertAndAnchors(cert, anchors, identityName)
			}),
		*healthcheck.NewChecker("tap API server cert is valid for at least 60 days").
			WithHintAnchor("l5d-tap-cert-not-expiring-soon").
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
			}),
		*healthcheck.NewChecker("tap API service is running").
			WithHintAnchor("l5d-tap-api").
			Warning().
			WithRetryDeadline(hc.RetryDeadline).
			WithCheck(func(ctx context.Context) error {
				return hc.CheckAPIService(ctx, linkerdTapAPIServiceName)
			}),
		*healthcheck.NewChecker("linkerd-viz pods are injected").
			WithHintAnchor("l5d-viz-pods-injection").
			Warning().
			WithCheck(func(ctx context.Context) error {
				pods, err := hc.KubeAPIClient().GetPodsByNamespace(ctx, hc.vizNamespace)
				if err != nil {
					return err
				}
				return healthcheck.CheckIfDataPlanePodsExist(pods)
			}),
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
				err = healthcheck.CheckForPods(pods, []string{"web", "tap", "metrics-api", "tap-injector"})
				if err != nil {
					return err
				}

				return healthcheck.CheckPodsRunning(pods, "")
			}),
		*healthcheck.NewChecker("viz extension proxies are healthy").
			WithHintAnchor("l5d-viz-proxy-healthy").
			Fatal().
			WithCheck(func(ctx context.Context) (err error) {
				return hc.CheckProxyHealth(ctx, hc.ControlPlaneNamespace, hc.vizNamespace)
			}),
		*healthcheck.NewChecker("viz extension proxies are up-to-date").
			WithHintAnchor("l5d-viz-proxy-cp-version").
			Warning().
			WithCheck(func(ctx context.Context) error {
				var err error
				if hc.VersionOverride != "" {
					hc.LatestVersions, err = version.NewChannels(hc.VersionOverride)
				} else {
					uuid := "unknown"
					if hc.UUID() != "" {
						uuid = hc.UUID()
					}
					hc.LatestVersions, err = version.GetLatestVersions(ctx, uuid, "cli")
				}
				if err != nil {
					return err
				}

				pods, err := hc.KubeAPIClient().GetPodsByNamespace(ctx, hc.vizNamespace)
				if err != nil {
					return err
				}

				return hc.CheckProxyVersionsUpToDate(pods)
			}),
		*healthcheck.NewChecker("viz extension proxies and cli versions match").
			WithHintAnchor("l5d-viz-proxy-cli-version").
			Warning().
			WithCheck(func(ctx context.Context) error {
				pods, err := hc.KubeAPIClient().GetPodsByNamespace(ctx, hc.vizNamespace)
				if err != nil {
					return err
				}

				return healthcheck.CheckIfProxyVersionsMatchWithCLI(pods)
			}),
		*healthcheck.NewChecker("prometheus is installed and configured correctly").
			WithHintAnchor("l5d-viz-prometheus").
			Warning().
			WithCheck(func(ctx context.Context) error {
				if hc.externalPrometheusURL != "" {
					return &healthcheck.SkipError{Reason: "prometheus is disabled"}
				}

				// Check for ClusterRoles
				err := healthcheck.CheckClusterRoles(ctx, hc.KubeAPIClient(), true, []string{fmt.Sprintf("linkerd-%s-prometheus", hc.vizNamespace)}, "")
				if err != nil {
					return err
				}

				// Check for ClusterRoleBindings
				err = healthcheck.CheckClusterRoleBindings(ctx, hc.KubeAPIClient(), true, []string{fmt.Sprintf("linkerd-%s-prometheus", hc.vizNamespace)}, "")
				if err != nil {
					return err
				}

				// Check for ConfigMap
				err = healthcheck.CheckConfigMaps(ctx, hc.KubeAPIClient(), hc.vizNamespace, true, []string{"prometheus-config"}, "")
				if err != nil {
					return err
				}

				// Check for relevant pods to be present
				pods, err := hc.KubeAPIClient().GetPodsByNamespace(ctx, hc.vizNamespace)
				if err != nil {
					return err
				}

				err = healthcheck.CheckForPods(pods, []string{"prometheus"})
				if err != nil {
					return err
				}

				return nil
			}),
		*healthcheck.NewChecker("can initialize the client").
			WithHintAnchor("l5d-viz-existence-client").
			Fatal().
			WithCheck(func(ctx context.Context) (err error) {
				hc.vizAPIClient, err = client.NewExternalClient(ctx, hc.vizNamespace, hc.KubeAPIClient())
				return
			}),
		*healthcheck.NewChecker("viz extension self-check").
			WithHintAnchor("l5d-viz-metrics-api").
			Fatal().
			// to avoid confusing users with a prometheus readiness error, we only show
			// "waiting for check to complete" while things converge. If after the timeout
			// it still hasn't converged, we show the real error (a 503 usually).
			WithRetryDeadline(hc.RetryDeadline).
			WithCheck(func(ctx context.Context) error {
				results, err := hc.vizAPIClient.SelfCheck(ctx, &pb.SelfCheckRequest{})
				if err != nil {
					return err
				}

				if len(results.GetResults()) == 0 {
					return errors.New("No results returned")
				}

				errs := []string{}
				for _, res := range results.GetResults() {
					if res.GetStatus() != pb.CheckStatus_OK {
						errs = append(errs, res.GetFriendlyMessageToUser())
					}
				}
				if len(errs) == 0 {
					return nil
				}

				errsStr := strings.Join(errs, "\n    ")
				return errors.New(errsStr)
			}),
	}, true)
}

// VizDataPlaneCategory returns a healthcheck.Category containing checkers
// to verify the data-plane metrics in prometheus and the tap injection
func (hc *HealthChecker) VizDataPlaneCategory() *healthcheck.Category {

	return healthcheck.NewCategory(LinkerdVizExtensionDataPlaneCheck, []healthcheck.Checker{
		*healthcheck.NewChecker("data plane namespace exists").
			WithHintAnchor("l5d-data-plane-exists").
			Fatal().
			WithCheck(func(ctx context.Context) error {
				if hc.DataPlaneNamespace == "" {
					// when checking proxies in all namespaces, this check is a no-op
					return nil
				}
				return hc.CheckNamespace(ctx, hc.DataPlaneNamespace, true)
			}),
		*healthcheck.NewChecker("data plane proxy metrics are present in Prometheus").
			WithHintAnchor("l5d-data-plane-prom").
			Warning().
			WithRetryDeadline(hc.RetryDeadline).
			WithCheck(func(ctx context.Context) (err error) {
				pods, err := hc.getDataPlanePodsFromVizAPI(ctx)
				if err != nil {
					return err
				}

				return validateDataPlanePodReporting(pods)
			}),
	}, true)
}

func (hc *HealthChecker) getDataPlanePodsFromVizAPI(ctx context.Context) ([]*pb.Pod, error) {

	req := &pb.ListPodsRequest{}
	if hc.DataPlaneNamespace != "" {
		req.Selector = &pb.ResourceSelection{
			Resource: &pb.Resource{
				Namespace: hc.DataPlaneNamespace,
			},
		}
	}

	resp, err := hc.VizAPIClient().ListPods(ctx, req)
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
