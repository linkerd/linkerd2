package cmd

import (
	"context"
	"crypto/x509"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiregistrationv1client "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/typed/apiregistration/v1"
)

const (

	// vizExtensionName is the name of Linkerd Viz extension
	vizExtensionName = "linkerd-viz"

	// linkerdVizExtensionCheck adds checks related to the Linkerd Viz etension
	linkerdVizExtensionCheck healthcheck.CategoryID = vizExtensionName

	// linkerdTapAPIServiceName is the name of the tap api service
	// This key is passed to checkApiService method to check whether
	// the api service is available or not
	linkerdTapAPIServiceName = "v1alpha1.tap.linkerd.io"

	tapOldTLSSecretName = "linkerd-tap-tls"
	tapTLSSecretName    = "linkerd-tap-k8s-tls"
)

type checkOptions struct {
	wait   time.Duration
	output string
}

func vizCategory(hc *healthcheck.HealthChecker) *healthcheck.Category {
	checkers := []healthcheck.Checker{}
	checkers = append(checkers,
		*healthcheck.NewChecker("linkerd-viz Namespace exists").
			WithHintAnchor("l5d-viz-ns-exists").
			Fatal().
			Warning().
			WithCheck(func(ctx context.Context) error {
				// Get viz Extension Namespace
				ns, err := getNamespaceOfExtension(vizExtensionName)
				if err != nil {
					return err
				}
				hc.VizNamespace = ns.Name
				return nil
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("linkerd-viz ClusterRoles exist").
			WithHintAnchor("l5d-viz-cr-exists").
			Fatal().
			Warning().
			WithCheck(func(ctx context.Context) error {
				return healthcheck.CheckClusterRoles(ctx, hc.KubeAPIClient(), true, []string{fmt.Sprintf("linkerd-%s-prometheus", hc.VizNamespace), fmt.Sprintf("linkerd-%s-tap", hc.VizNamespace)}, "")
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("linkerd-viz ClusterRoleBindings exist").
			WithHintAnchor("l5d-viz-crb-exists").
			Fatal().
			Warning().
			WithCheck(func(ctx context.Context) error {
				return healthcheck.CheckClusterRoleBindings(ctx, hc.KubeAPIClient(), true, []string{fmt.Sprintf("linkerd-%s-prometheus", hc.VizNamespace), fmt.Sprintf("linkerd-%s-tap", hc.VizNamespace)}, "")
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("linkerd-viz ConfigMaps exist").
			WithHintAnchor("l5d-viz-cm-exists").
			Fatal().
			Warning().
			WithCheck(func(ctx context.Context) error {
				return healthcheck.CheckConfigMaps(ctx, hc.KubeAPIClient(), hc.VizNamespace, true, []string{"linkerd-prometheus-config", "linkerd-grafana-config"}, "")
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
				cert, err := hc.FetchCredsFromSecret(ctx, hc.VizNamespace, tapTLSSecretName)
				if kerrors.IsNotFound(err) {
					cert, err = hc.FetchCredsFromOldSecret(ctx, hc.VizNamespace, tapOldTLSSecretName)
				}
				if err != nil {
					return err
				}

				identityName := fmt.Sprintf("linkerd-tap.%s.svc", hc.VizNamespace)
				return hc.CheckCertAndAnchors(cert, anchors, identityName)
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("tap API server cert is valid for at least 60 days").
			WithHintAnchor("l5d-webhook-cert-not-expiring-soon").
			Warning().
			WithCheck(func(ctx context.Context) error {
				cert, err := hc.FetchCredsFromSecret(ctx, hc.VizNamespace, tapTLSSecretName)
				if kerrors.IsNotFound(err) {
					cert, err = hc.FetchCredsFromOldSecret(ctx, hc.VizNamespace, tapOldTLSSecretName)
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
				pods, err := hc.KubeAPIClient().GetPodsByNamespace(ctx, hc.VizNamespace)
				if err != nil {
					return err
				}

				// Check for relevant pods to be present
				err = healthcheck.CheckForPods(pods, []string{"linkerd-grafana", "linkerd-prometheus", "linkerd-web", "linkerd-tap"})
				if err != nil {
					return err
				}

				return healthcheck.CheckPodsRunning(pods, "")
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("linkerd-viz pods are injected").
			WithHintAnchor("l5d-viz-pods-injection").
			Warning().
			WithCheck(func(ctx context.Context) error {
				pods, err := hc.KubeAPIClient().GetPodsByNamespace(ctx, hc.VizNamespace)
				if err != nil {
					return err
				}
				return healthcheck.CheckIfDataPlanePodsExist(pods)
			}))

	// TODO: Add dataplane metrics in prometheus check
	return healthcheck.NewCategory(linkerdVizExtensionCheck, checkers, true)
}

func newCheckOptions() *checkOptions {
	return &checkOptions{
		wait:   300 * time.Second,
		output: healthcheck.TableOutput,
	}
}

func (options *checkOptions) validate() error {
	if options.output != healthcheck.TableOutput && options.output != healthcheck.JSONOutput {
		return fmt.Errorf("Invalid output type '%s'. Supported output types are: %s, %s", options.output, healthcheck.JSONOutput, healthcheck.TableOutput)
	}
	return nil
}

func newCmdCheck() *cobra.Command {
	options := newCheckOptions()
	cmd := &cobra.Command{
		Use:   "check [flags]",
		Args:  cobra.NoArgs,
		Short: "Check the Linkerd Viz extension for potential problems",
		Long: `Check the Linkerd Viz extension for potential problems.

The check command will perform a series of checks to validate that the Linkerd Viz
extension is configured correctly. If the command encounters a failure it will
print additional information about the failure and exit with a non-zero exit
code.`,
		Example: `  # Check that the viz extension is up and running
  linkerd viz check`,
		RunE: func(cmd *cobra.Command, args []string) error {

			return configureAndRunChecks(stdout, stderr, options)
		},
	}

	cmd.PersistentFlags().StringVarP(&options.output, "output", "o", options.output, "Output format. One of: basic, json")
	cmd.PersistentFlags().DurationVar(&options.wait, "wait", options.wait, "Maximum allowed time for all tests to pass")

	return cmd
}

func configureAndRunChecks(wout io.Writer, werr io.Writer, options *checkOptions) error {
	err := options.validate()
	if err != nil {
		return fmt.Errorf("Validation error when executing check command: %v", err)
	}

	checks := []healthcheck.CategoryID{
		healthcheck.KubernetesAPIChecks,
		healthcheck.LinkerdControlPlaneExistenceChecks,
		linkerdVizExtensionCheck,
	}

	hc := healthcheck.NewHealthChecker(checks, &healthcheck.Options{
		ControlPlaneNamespace: controlPlaneNamespace,
		KubeConfig:            kubeconfigPath,
		KubeContext:           kubeContext,
		Impersonate:           impersonate,
		ImpersonateGroup:      impersonateGroup,
		APIAddr:               apiAddr,
		RetryDeadline:         time.Now().Add(options.wait),
	})

	hc.AppendCategories(*vizCategory(hc))

	success := healthcheck.RunChecks(wout, werr, hc, options.output)

	if !success {
		os.Exit(1)
	}

	return nil
}

func getNamespaceOfExtension(name string) (*corev1.Namespace, error) {
	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return nil, err
	}

	namespaces, err := kubeAPI.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{LabelSelector: k8s.LinkerdExtensionLabel})
	if err != nil {
		return nil, err
	}

	for _, ns := range namespaces.Items {
		if ns.Labels[k8s.LinkerdExtensionLabel] == name {
			return &ns, err
		}
	}
	return nil, fmt.Errorf("could not find the linkerd-viz extension. It can be installed by running `linkerd viz install | kubectl apply -f -`")
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
