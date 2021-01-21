package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (

	// jaegerExtensionName is the name of jaeger extension
	jaegerExtensionName = "linkerd-jaeger"

	// linkerdJaegerExtensionCheck adds checks related to the jaeger extension
	linkerdJaegerExtensionCheck healthcheck.CategoryID = jaegerExtensionName
)

var (
	jaegerNamespace string
)

type checkOptions struct {
	wait   time.Duration
	output string
}

func jaegerCategory(hc *healthcheck.HealthChecker) (*healthcheck.Category, error) {

	kubeAPI, err := k8s.NewAPI(hc.KubeConfig, hc.KubeContext, hc.Impersonate, hc.ImpersonateGroup, 0)
	if err != nil {
		return nil, err
	}

	checkers := []healthcheck.Checker{}
	checkers = append(checkers,
		*healthcheck.NewChecker("collector and jaeger service account exists").
			WithHintAnchor("l5d-jaeger-sc-exists").
			Fatal().
			Warning().
			WithCheck(func(ctx context.Context) error {
				// Check for Collector Service Account
				return healthcheck.CheckServiceAccounts(ctx, kubeAPI, []string{"collector", "jaeger"}, jaegerNamespace, "")
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("collector config map exists").
			WithHintAnchor("l5d-jaeger-oc-cm-exists").
			Warning().
			WithCheck(func(ctx context.Context) error {
				// Check for Jaeger Service Account
				_, err = kubeAPI.CoreV1().ConfigMaps(jaegerNamespace).Get(ctx, "collector-config", metav1.GetOptions{})
				if err != nil {
					return err
				}
				return nil
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("collector pod is running").
			WithHintAnchor("l5d-jaeger-collector-running").
			Fatal().
			WithRetryDeadline(hc.RetryDeadline).
			SurfaceErrorOnRetry().
			WithCheck(func(ctx context.Context) error {
				// Check for Collector pod
				podList, err := kubeAPI.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "component=collector"})
				if err != nil {
					return err
				}
				return healthcheck.CheckPodsRunning(podList.Items, fmt.Sprintf("No collector pods found in the %s namespace", namespace))
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("jaeger pod is running").
			WithHintAnchor("l5d-jaeger-jaeger-running").
			Fatal().
			WithRetryDeadline(hc.RetryDeadline).
			SurfaceErrorOnRetry().
			WithCheck(func(ctx context.Context) error {
				// Check for Jaeger pod
				podList, err := kubeAPI.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "component=jaeger"})
				if err != nil {
					return err
				}
				return healthcheck.CheckPodsRunning(podList.Items, fmt.Sprintf("No jaeger pods found in the %s namespace", namespace))
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("jaeger injector pod is running").
			WithHintAnchor("l5d-jaeger-jaeger-running").
			Fatal().
			WithRetryDeadline(hc.RetryDeadline).
			SurfaceErrorOnRetry().
			WithCheck(func(ctx context.Context) error {
				// Check for Jaeger Injector pod
				podList, err := kubeAPI.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "component=jaeger-injector"})
				if err != nil {
					return err
				}
				return healthcheck.CheckPodsRunning(podList.Items, fmt.Sprintf("No jaeger injector pods found in the %s namespace", namespace))
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("jaeger extension pods are injected").
			WithHintAnchor("l5d-jaeger-pods-injection").
			Warning().
			WithCheck(func(ctx context.Context) error {
				// Check if Jaeger Extension pods have been injected
				pods, err := kubeAPI.GetPodsByNamespace(ctx, jaegerNamespace)
				if err != nil {
					return err
				}
				return healthcheck.CheckIfDataPlanePodsExist(pods)
			}))

	return healthcheck.NewCategory(linkerdJaegerExtensionCheck, checkers, true), nil
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
		Short: "Check the Jaeger extension for potential problems",
		Long: `Check the Jaeger extension for potential problems.

The check command will perform a series of checks to validate that the Jaeger
extension is configured correctly. If the command encounters a failure it will
print additional information about the failure and exit with a non-zero exit
code.`,
		Example: `  # Check that the Jaeger extension is up and running
  linkerd jaeger check`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get the Jaeger extension namespace
			kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			ns, err := kubeAPI.GetNamespaceWithExtensionLabel(context.Background(), jaegerExtensionName)
			if err != nil {
				err = fmt.Errorf("%w; install by running `linkerd jaeger install | kubectl apply -f -`", err)
				fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}
			jaegerNamespace = ns.Name
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
		linkerdJaegerExtensionCheck,
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

	category, err := jaegerCategory(hc)
	if err != nil {
		return err
	}
	hc.AppendCategories(*category)

	success := healthcheck.RunChecks(wout, werr, hc, options.output)

	if !success {
		os.Exit(1)
	}

	return nil
}
