package healthcheck

import (
	"context"
	"crypto/x509"
	"fmt"
	"strings"

	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/inject"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/viz/metrics-api/client"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiregistrationv1client "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/typed/apiregistration/v1"
)

const (
	// LinkerdVizExtensionCheck adds checks related to the Linkerd Viz extension
	LinkerdVizExtensionCheck healthcheck.CategoryID = "linkerd-viz"

	// LinkerdVizExtensionDataPlaneCheck adds checks related to dataplane for the linkerd-viz extension
	LinkerdVizExtensionDataPlaneCheck healthcheck.CategoryID = "linkerd-viz-data-plane"

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

	vizCategories := hc.vizCategory()
	checkMap := map[healthcheck.CategoryID]struct{}{}
	for _, category := range categoryIDs {
		checkMap[category] = struct{}{}
	}

	// Append Only the categories that aare passed
	for i := range vizCategories {
		if _, ok := checkMap[vizCategories[i].ID]; ok {
			parentHC.AppendCategories(vizCategories[i])
		}
	}

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

func (hc *HealthChecker) vizCategory() []healthcheck.Category {
	return []healthcheck.Category{
		*healthcheck.NewCategory(LinkerdVizExtensionCheck, []healthcheck.Checker{
			*healthcheck.NewChecker("linkerd-viz Namespace exists").
				WithHintAnchor("l5d-viz-ns-exists").
				Fatal().
				WithCheck(func(ctx context.Context) error {
					vizNs, err := hc.KubeAPIClient().GetNamespaceWithExtensionLabel(ctx, "linkerd-viz")
					if err == nil {
						hc.vizNamespace = vizNs.Name
					}
					return err
				}),
			*healthcheck.NewChecker("linkerd-viz ClusterRoles exist").
				WithHintAnchor("l5d-viz-cr-exists").
				Fatal().
				Warning().
				WithCheck(func(ctx context.Context) error {
					return healthcheck.CheckClusterRoles(ctx, hc.KubeAPIClient(), true, []string{fmt.Sprintf("linkerd-%s-prometheus", hc.vizNamespace), fmt.Sprintf("linkerd-%s-tap", hc.vizNamespace)}, "")
				}),
			*healthcheck.NewChecker("linkerd-viz ClusterRoleBindings exist").
				WithHintAnchor("l5d-viz-crb-exists").
				Fatal().
				Warning().
				WithCheck(func(ctx context.Context) error {
					return healthcheck.CheckClusterRoleBindings(ctx, hc.KubeAPIClient(), true, []string{fmt.Sprintf("linkerd-%s-prometheus", hc.vizNamespace), fmt.Sprintf("linkerd-%s-tap", hc.vizNamespace)}, "")
				}),
			*healthcheck.NewChecker("linkerd-viz ConfigMaps exist").
				WithHintAnchor("l5d-viz-cm-exists").
				Fatal().
				Warning().
				WithCheck(func(ctx context.Context) error {
					return healthcheck.CheckConfigMaps(ctx, hc.KubeAPIClient(), hc.vizNamespace, true, []string{"linkerd-prometheus-config", "linkerd-grafana-config"}, "")
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

					identityName := fmt.Sprintf("linkerd-tap.%s.svc", hc.vizNamespace)
					return hc.CheckCertAndAnchors(cert, anchors, identityName)
				}),
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
					err = healthcheck.CheckForPods(pods, []string{"linkerd-grafana", "linkerd-prometheus", "linkerd-web", "linkerd-tap"})
					if err != nil {
						return err
					}

					return healthcheck.CheckPodsRunning(pods, "")
				}),
			*healthcheck.NewChecker("can initialize the client").
				WithHintAnchor("l5d-viz-existence-client").
				Fatal().
				WithCheck(func(ctx context.Context) (err error) {
					hc.vizAPIClient, err = client.NewExternalClient(ctx, hc.vizNamespace, hc.KubeAPIClient())
					return
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

					// TODO: Check if prometheus is present

					return validateDataPlanePodReporting(pods)
				}),
			*healthcheck.NewChecker("viz extension self-check").
				WithHintAnchor("l5d-api-control-api").
				Fatal().
				// to avoid confusing users with a prometheus readiness error, we only show
				// "waiting for check to complete" while things converge. If after the timeout
				// it still hasn't converged, we show the real error (a 503 usually).
				WithRetryDeadline(hc.RetryDeadline).
				WithCheckRPC(func(ctx context.Context) (*healthcheckPb.SelfCheckResponse, error) {
					return hc.vizAPIClient.SelfCheck(ctx, &healthcheckPb.SelfCheckRequest{})
				}),
		}, true),

		*healthcheck.NewCategory(LinkerdVizExtensionDataPlaneCheck, []healthcheck.Checker{
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
			*healthcheck.NewChecker("data-plane pods have the correct tap configuration").
				WithHintAnchor("l5d-viz-data-plane-tap").
				Fatal().
				WithCheck(func(ctx context.Context) error {
					pods, err := hc.GetDataPlanePods(ctx)
					if err != nil {
						return err
					}

					// Check environment specific configuration based on labels
					// Do not warn for the viz extension or core cp pods
					return hc.checkForTapConfiguration(ctx, pods)
				}),
		}, true),
	}
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

// checkForTapConfiguration checks if the tap env variable is present in the linkerd proxy
// only for the pods with tap enabled
func (hc *HealthChecker) checkForTapConfiguration(ctx context.Context, pods []corev1.Pod) error {
	var podsWithoutTap []string
	for _, pod := range pods {
		if pod.Namespace != hc.vizNamespace && pod.Namespace != hc.ControlPlaneNamespace {
			ns, err := hc.KubeAPIClient().CoreV1().Namespaces().Get(ctx, pod.Namespace, metav1.GetOptions{})
			if err != nil {
				return err
			}
			// Check if Tap is disabled
			if !k8s.IsTapDisabled(pod) && !k8s.IsTapDisabled(ns) {
				// Check for Configuration
				if !k8s.CheckForEnv(k8s.GetProxy(pod), inject.TapSvcEnvKey) {
					podsWithoutTap = append(podsWithoutTap, pod.Name)
				}
			}
		}
	}

	if len(podsWithoutTap) > 0 {
		podList := strings.Join(podsWithoutTap, "\n")
		return fmt.Errorf("Some data plane pods do not have tap configured:\n%s", podList)
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
