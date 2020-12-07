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

	// jaegerExtensionName is the name fo jaeger extension
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

func jaegerCategory() (*healthcheck.Category, error) {

	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return nil, err
	}

	checkers := []healthcheck.Checker{}
	checkers = append(checkers,
		*healthcheck.NewChecker("collector service account exists", "", false, true, time.Time{}, false).
			WithCheck(func(ctx context.Context) error {
				// Check for Collector Service Account
				return healthcheck.CheckServiceAccounts(ctx, kubeAPI, []string{"collector"}, jaegerNamespace, "")
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("jaeger service account exists", "", false, true, time.Time{}, false).
			WithCheck(func(ctx context.Context) error {
				// Check for Jaeger Service Account
				return healthcheck.CheckServiceAccounts(ctx, kubeAPI, []string{"jaeger"}, jaegerNamespace, "")
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("collector config map exists", "", false, true, time.Time{}, false).
			WithCheck(func(ctx context.Context) error {
				// Check for Jaeger Service Account
				_, err = kubeAPI.CoreV1().ConfigMaps(jaegerNamespace).Get(ctx, "collector-config", metav1.GetOptions{})
				if err != nil {
					return err
				}
				return nil
			}))
	checkers = append(checkers,
		*healthcheck.NewChecker("collector pod is running", "", false, true, time.Time{}, false).
			WithCheck(func(ctx context.Context) error {
				// Check for Collector pod
				pods, err := kubeAPI.GetPods(ctx, jaegerNamespace, "component=collector")
				if err != nil {
					return err
				}
				return checkPodsStatus(pods)
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("jaeger pod is running", "", false, true, time.Time{}, false).
			WithCheck(func(ctx context.Context) error {
				// Check for Jaeger pod
				pods, err := kubeAPI.GetPods(ctx, jaegerNamespace, "component=jaeger")
				if err != nil {
					return err
				}
				return checkPodsStatus(pods)
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("jaeger extension pods are injected", "", false, true, time.Time{}, false).
			WithCheck(func(ctx context.Context) error {
				// Check for Jaeger pod
				pods, err := kubeAPI.GetPodsByNamespace(ctx, jaegerNamespace)
				if err != nil {
					return err
				}

				return checkIfDataPlanePodsExist(pods)
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
		Long: `Check the jaeger extension for potential problems.

The check command will perform a series of checks to validate that the Jaeger extension
 is configured correctly. If the command encounters a
failure it will print additional information about the failure and exit with a
non-zero exit code.`,
		Example: `  # Check that the Jaeger extension is up and running
  linkerd jaeger check`,
		RunE: func(cmd *cobra.Command, args []string) error {

			// Get jaeger Extension Namespace
			ns, err := getNamespaceOfExtension("linkerd-jaeger")
			if err != nil {
				fmt.Fprint(os.Stderr, err.Error())
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
		MultiCluster:          false,
	})

	category, err := jaegerCategory()
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
	return nil, fmt.Errorf("Could not find the namespace for extension %s", name)
}

func checkIfDataPlanePodsExist(pods []corev1.Pod) error {
	for _, pod := range pods {
		proxyContainer := false
		for _, containerSpec := range pod.Spec.Containers {
			if containerSpec.Name == k8s.ProxyContainerName {
				proxyContainer = true
			}
		}

		if !proxyContainer {
			return fmt.Errorf("could not find proxy container for %s pod", pod.Name)
		}
	}

	return nil
}

// checkPodsStatus checks if the pod is in running state
func checkPodsStatus(pods []corev1.Pod) error {
	for _, pod := range pods {
		if pod.Status.Phase != "Running" {
			return fmt.Errorf("%s status is %s", pod.Name, pod.Status.Phase)
		}
	}
	return nil
}
