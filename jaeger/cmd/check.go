package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (

	// JaegerExtensionName is the name of jaeger extension
	JaegerExtensionName = "jaeger"

	// linkerdJaegerExtensionCheck adds checks related to the jaeger extension
	linkerdJaegerExtensionCheck healthcheck.CategoryID = "linkerd-jaeger"
)

var (
	jaegerNamespace string
)

type checkOptions struct {
	wait      time.Duration
	output    string
	proxy     bool
	namespace string
}

func jaegerCategory(hc *healthcheck.HealthChecker) *healthcheck.Category {

	checkers := []healthcheck.Checker{}

	checkers = append(checkers,
		*healthcheck.NewChecker("linkerd-jaeger extension Namespace exists").
			WithHintAnchor("l5d-jaeger-ns-exists").
			Fatal().
			WithCheck(func(ctx context.Context) error {
				// Get  jaeger Extension Namespace
				ns, err := hc.KubeAPIClient().GetNamespaceWithExtensionLabel(ctx, JaegerExtensionName)
				if err != nil {
					return err
				}
				jaegerNamespace = ns.Name
				return nil
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("collector and jaeger service account exists").
			WithHintAnchor("l5d-jaeger-sc-exists").
			Fatal().
			Warning().
			WithCheck(func(ctx context.Context) error {
				// Check for Collector Service Account
				return healthcheck.CheckServiceAccounts(ctx, hc.KubeAPIClient(), []string{"collector", "jaeger"}, jaegerNamespace, "")
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("collector config map exists").
			WithHintAnchor("l5d-jaeger-oc-cm-exists").
			Warning().
			WithCheck(func(ctx context.Context) error {
				// Check for Jaeger Service Account
				_, err := hc.KubeAPIClient().CoreV1().ConfigMaps(jaegerNamespace).Get(ctx, "collector-config", metav1.GetOptions{})
				if err != nil {
					return err
				}
				return nil
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("jaeger extension pods are injected").
			WithHintAnchor("l5d-jaeger-pods-injection").
			Warning().
			WithCheck(func(ctx context.Context) error {
				// Check if Jaeger Extension pods have been injected
				pods, err := hc.KubeAPIClient().GetPodsByNamespace(ctx, jaegerNamespace)
				if err != nil {
					return err
				}
				return healthcheck.CheckIfDataPlanePodsExist(pods)
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("jaeger extension pods are running").
			WithHintAnchor("l5d-jaeger-pods-running").
			Fatal().
			WithRetryDeadline(hc.RetryDeadline).
			SurfaceErrorOnRetry().
			WithCheck(func(ctx context.Context) error {
				pods, err := hc.KubeAPIClient().GetPodsByNamespace(ctx, jaegerNamespace)
				if err != nil {
					return err
				}

				// Check for relevant pods to be present
				err = healthcheck.CheckForPods(pods, []string{"collector", "jaeger", "jaeger-injector"})
				if err != nil {
					return err
				}

				return healthcheck.CheckPodsRunning(pods, "")

			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("jaeger extension proxies are healthy").
			WithHintAnchor("l5d-jaeger-proxy-healthy").
			Fatal().
			WithRetryDeadline(hc.RetryDeadline).
			SurfaceErrorOnRetry().
			WithCheck(func(ctx context.Context) error {
				return hc.CheckProxyHealth(ctx, hc.ControlPlaneNamespace, jaegerNamespace)
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("jaeger extension proxies are up-to-date").
			WithHintAnchor("l5d-jaeger-proxy-cp-version").
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

				pods, err := hc.KubeAPIClient().GetPodsByNamespace(ctx, jaegerNamespace)
				if err != nil {
					return err
				}

				return hc.CheckProxyVersionsUpToDate(pods)
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("jaeger extension proxies and cli versions match").
			WithHintAnchor("l5d-jaeger-proxy-cli-version").
			Warning().
			WithCheck(func(ctx context.Context) error {
				pods, err := hc.KubeAPIClient().GetPodsByNamespace(ctx, jaegerNamespace)
				if err != nil {
					return err
				}

				return healthcheck.CheckIfProxyVersionsMatchWithCLI(pods)
			}))

	return healthcheck.NewCategory(linkerdJaegerExtensionCheck, checkers, true)
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

// NewCmdCheck generates a new cobra command for the jaeger extension.
func NewCmdCheck() *cobra.Command {
	options := newCheckOptions()
	cmd := &cobra.Command{
		Use:   "check [flags]",
		Args:  cobra.NoArgs,
		Short: "Check the Jaeger extension for potential problems",
		Long: `Check the Jaeger extension for potential problems.

The check command will perform a series of checks to validate that the Jaeger
extension is configured correctly. If the command encounters a failure it will
print additional information about the failure and exit with a non-zero exit
code.`,
		Example: `  # Check that the Jaeger extension is up and running
  linkerd jaeger check`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return configureAndRunChecks(stdout, stderr, options)
		},
	}

	cmd.Flags().StringVarP(&options.output, "output", "o", options.output, "Output format. One of: basic, json")
	cmd.Flags().DurationVar(&options.wait, "wait", options.wait, "Maximum allowed time for all tests to pass")
	cmd.Flags().BoolVar(&options.proxy, "proxy", options.proxy, "Also run data-plane checks, to determine if the data plane is healthy")
	cmd.Flags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace to use for --proxy checks (default: all namespaces)")

	return cmd
}

func configureAndRunChecks(wout io.Writer, werr io.Writer, options *checkOptions) error {
	err := options.validate()
	if err != nil {
		return fmt.Errorf("Validation error when executing check command: %v", err)
	}

	hc := healthcheck.NewHealthChecker([]healthcheck.CategoryID{}, &healthcheck.Options{
		ControlPlaneNamespace: controlPlaneNamespace,
		KubeConfig:            kubeconfigPath,
		KubeContext:           kubeContext,
		Impersonate:           impersonate,
		ImpersonateGroup:      impersonateGroup,
		APIAddr:               apiAddr,
		RetryDeadline:         time.Now().Add(options.wait),
		DataPlaneNamespace:    options.namespace,
	})

	err = hc.InitializeKubeAPIClient()
	if err != nil {
		err = fmt.Errorf("Error initializing k8s API client: %s", err)
		fmt.Fprintln(werr, err)
		os.Exit(1)
	}

	hc.AppendCategories(jaegerCategory(hc))

	success := healthcheck.RunChecks(wout, werr, hc, options.output)

	if !success {
		os.Exit(1)
	}

	return nil
}
