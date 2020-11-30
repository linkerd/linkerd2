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
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// LinkerdJaegerExtensionCheck adds checks related to the jaeger extension
	LinkerdJaegerExtensionCheck healthcheck.CategoryID = "linkerd-jaeger"
)

type checkOptions struct {
	wait      time.Duration
	namespace string
	output    string
}

func jaegerCategory() *healthcheck.Category {

	checkers := []healthcheck.Checker{}
	checkers = append(checkers,
		*healthcheck.NewChecker("collector service account exists", "", false, true, time.Time{}, false).
			WithCheck(func(ctx context.Context) error {
				// Check for Collector Service Account
				kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
				if err != nil {
					return err
				}

				return healthcheck.CheckServiceAccounts(ctx, kubeAPI, []string{"collector"}, "linkerd-jaeger", "")
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("jaeger service account exists", "", false, true, time.Time{}, false).
			WithCheck(func(ctx context.Context) error {
				// Check for Jaeger Service Account
				kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
				if err != nil {
					return err
				}

				return healthcheck.CheckServiceAccounts(ctx, kubeAPI, []string{"jaeger"}, "linkerd-jaeger", "")
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("collector config map exists", "", false, true, time.Time{}, false).
			WithCheck(func(ctx context.Context) error {
				// Check for Jaeger Service Account
				kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
				if err != nil {
					return err
				}

				_, err = kubeAPI.CoreV1().ConfigMaps("linkerd-jaeger").Get(ctx, "collector-config", metav1.GetOptions{})
				if err != nil {
					return err
				}
				return nil
			}))
	checkers = append(checkers,
		*healthcheck.NewChecker("collector pod is running", "", false, true, time.Time{}, false).
			WithCheck(func(ctx context.Context) error {
				// Check for Collector pod
				kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
				if err != nil {
					return err
				}

				pods, err := kubeAPI.GetPodsByNamespace(ctx, "linkerd-jaeger")
				if err != nil {
					return err
				}

				return healthcheck.CheckContainerRunning(pods, "collector")
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("jaeger pod is running", "", false, true, time.Time{}, false).
			WithCheck(func(ctx context.Context) error {
				// Check for Jaeger pod
				kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
				if err != nil {
					return err
				}

				pods, err := kubeAPI.GetPodsByNamespace(ctx, "linkerd-jaeger")
				if err != nil {
					return err
				}

				return healthcheck.CheckContainerRunning(pods, "jaeger")
			}))

	return healthcheck.NewCategory(LinkerdJaegerExtensionCheck, checkers, true)
}

func newCheckOptions() *checkOptions {
	return &checkOptions{
		wait:      300 * time.Second,
		namespace: "",
		output:    healthcheck.TableOutput,
	}
}

// checkFlagSet specifies flags allowed with and without `config`
func (options *checkOptions) checkFlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("check", pflag.ExitOnError)

	flags.StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace to use for --proxy checks (default: all namespaces)")
	flags.StringVarP(&options.output, "output", "o", options.output, "Output format. One of: basic, json")
	flags.DurationVar(&options.wait, "wait", options.wait, "Maximum allowed time for all tests to pass")

	return flags
}

func (options *checkOptions) validate() error {
	if options.output != healthcheck.TableOutput && options.output != healthcheck.JSONOutput {
		return fmt.Errorf("Invalid output type '%s'. Supported output types are: %s, %s", options.output, healthcheck.JSONOutput, healthcheck.TableOutput)
	}
	return nil
}

func newCmdCheck() *cobra.Command {
	options := newCheckOptions()
	checkFlags := options.checkFlagSet()

	cmd := &cobra.Command{
		Use:   "check [flags]",
		Args:  cobra.NoArgs,
		Short: "Check the Jaeger extension for potential problems",
		Long: `Check the jaeger extension for potential problems.

The check command will perform a series of checks to validate that the Jaeger extension
 is configured correctly. If the command encounters a
failure it will print additional information about the failure and exit with a
non-zero exit code.`,
		Example: `  # Check that the Jaeger extension is up and running
  linkerd jaeger check`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return configureAndRunChecks(stdout, stderr, options)
		},
	}

	cmd.PersistentFlags().AddFlagSet(checkFlags)

	return cmd
}

func configureAndRunChecks(wout io.Writer, werr io.Writer, options *checkOptions) error {
	err := options.validate()
	if err != nil {
		return fmt.Errorf("Validation error when executing check command: %v", err)
	}

	checks := []healthcheck.CategoryID{
		LinkerdJaegerExtensionCheck,
	}

	hc := healthcheck.NewHealthChecker(checks, &healthcheck.Options{
		ControlPlaneNamespace: controlPlaneNamespace,
		KubeConfig:            kubeconfigPath,
		KubeContext:           kubeContext,
		Impersonate:           impersonate,
		ImpersonateGroup:      impersonateGroup,
		APIAddr:               apiAddr,
		RetryDeadline:         time.Now().Add(options.wait),
		MultiCluster:          false,
	})

	hc.AppendCategories(*jaegerCategory())

	success := healthcheck.RunChecks(wout, werr, hc, options.output)

	if !success {
		os.Exit(1)
	}

	return nil
}
