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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (

	// vizExtensionName is the name of Linkerd Viz extension
	vizExtensionName = "linkerd-viz"

	// linkerdVizExtensionCheck adds checks related to the Linkerd Viz etension
	linkerdVizExtensionCheck healthcheck.CategoryID = vizExtensionName
)

var (
	vizNamespace string
)

type checkOptions struct {
	wait   time.Duration
	output string
}

func vizCategory(hc *healthcheck.HealthChecker) (*healthcheck.Category, error) {

	kubeAPI, err := k8s.NewAPI(hc.KubeConfig, hc.KubeContext, hc.Impersonate, hc.ImpersonateGroup, 0)
	if err != nil {
		return nil, err
	}

	checkers := []healthcheck.Checker{}
	checkers = append(checkers,
		*healthcheck.NewChecker("linkerd-viz Namespace exists").
			WithHintAnchor("l5d-viz-ns-exists").
			Fatal().
			Warning().
			WithCheck(func(ctx context.Context) error {
				return hc.CheckNamespace(ctx, vizNamespace, true)
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("linkerd-viz ClusterRoles exist").
			WithHintAnchor("l5d-viz-sc-exists").
			Fatal().
			Warning().
			WithCheck(func(ctx context.Context) error {
				return healthcheck.CheckClusterRoles(ctx, kubeAPI, true, []string{fmt.Sprintf("linkerd-%s-prometheus", vizNamespace), fmt.Sprintf("linkerd-%s-tap", vizNamespace)}, "")
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("linkerd-viz ClusterRoleBindings exist").
			WithHintAnchor("l5d-viz-sc-exists").
			Fatal().
			Warning().
			WithCheck(func(ctx context.Context) error {
				return healthcheck.CheckClusterRoleBindings(ctx, kubeAPI, true, []string{fmt.Sprintf("linkerd-%s-prometheus", vizNamespace), fmt.Sprintf("linkerd-%s-tap", vizNamespace), "linkerd-web"}, "")
			}))

	//TODO: add tap webhook certs check

	checkers = append(checkers,
		*healthcheck.NewChecker("viz extension pods are running").
			WithHintAnchor("l5d-viz-collector-running").
			Warning().
			WithRetryDeadline(hc.RetryDeadline).
			SurfaceErrorOnRetry().
			WithCheck(func(ctx context.Context) error {
				pods, err := kubeAPI.GetPodsByNamespace(ctx, vizNamespace)
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
				pods, err := kubeAPI.GetPodsByNamespace(ctx, vizNamespace)
				if err != nil {
					return err
				}
				return healthcheck.CheckIfDataPlanePodsExist(pods)
			}))

	return healthcheck.NewCategory(linkerdVizExtensionCheck, checkers, true), nil
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

			// Get viz Extension Namespace
			ns, err := getNamespaceOfExtension(vizExtensionName)
			if err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}
			vizNamespace = ns.Name

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

	category, err := vizCategory(hc)
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
	return nil, fmt.Errorf("could not find the linkerd-viz extension. it can be installed by running `linkerd viz install | kubectl apply -f -`")
}
