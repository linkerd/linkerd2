package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (

	// JaegerExtensionName is the name of jaeger extension
	JaegerExtensionName = "jaeger"

	// JaegerLegacyExtension is the name of the jaeger extension prior to
	// stable-2.10.0 when the linkerd prefix was removed.
	JaegerLegacyExtension = "linkerd-jaeger"

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
		*healthcheck.NewChecker("jaeger injector pods are running").
			WithHintAnchor("l5d-jaeger-pods-running").
			Warning().
			WithRetryDeadline(hc.RetryDeadline).
			SurfaceErrorOnRetry().
			WithCheck(func(ctx context.Context) error {
				podList, err := hc.KubeAPIClient().CoreV1().Pods(jaegerNamespace).List(ctx, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s", k8s.LinkerdExtensionLabel, JaegerExtensionName),
				})
				if err != nil {
					return err
				}

				// Check for relevant pods to be present
				err = healthcheck.CheckForPods(podList.Items, []string{"jaeger-injector"})
				if err != nil {
					return err
				}

				return healthcheck.CheckPodsRunning(podList.Items, jaegerNamespace)
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("jaeger extension proxies are healthy").
			WithHintAnchor("l5d-jaeger-proxy-healthy").
			Warning().
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
	if options.output != healthcheck.TableOutput && options.output != healthcheck.JSONOutput && options.output != healthcheck.ShortOutput {
		return fmt.Errorf("Invalid output type '%s'. Supported output types are: %s, %s, %s", options.output, healthcheck.JSONOutput, healthcheck.TableOutput, healthcheck.ShortOutput)
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

	cmd.Flags().StringVarP(&options.output, "output", "o", options.output, "Output format. One of: table, json, short")
	cmd.Flags().DurationVar(&options.wait, "wait", options.wait, "Maximum allowed time for all tests to pass")
	cmd.Flags().BoolVar(&options.proxy, "proxy", options.proxy, "Also run data-plane checks, to determine if the data plane is healthy")
	cmd.Flags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace to use for --proxy checks (default: all namespaces)")

	pkgcmd.ConfigureNamespaceFlagCompletion(
		cmd, []string{"namespace"},
		kubeconfigPath, impersonate, impersonateGroup, kubeContext)
	pkgcmd.ConfigureOutputFlagCompletion(cmd)

	return cmd
}

func configureAndRunChecks(wout io.Writer, werr io.Writer, options *checkOptions) error {
	err := options.validate()
	if err != nil {
		return fmt.Errorf("Validation error when executing check command: %w", err)
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
		fmt.Fprintf(werr, "Error initializing k8s API client: %s\n", err)
		os.Exit(1)
	}

	hc.AppendCategories(jaegerCategory(hc))

	success, warning := healthcheck.RunChecks(wout, werr, hc, options.output)
	healthcheck.PrintChecksResult(wout, options.output, success, warning)

	if !success {
		os.Exit(1)
	}

	return nil
}
