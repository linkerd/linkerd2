package cmd

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	vizHealthCheck "github.com/linkerd/linkerd2/viz/pkg/healthcheck"
	"github.com/spf13/cobra"
)

type checkOptions struct {
	proxy     bool
	wait      time.Duration
	namespace string
	output    string
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

// NewCmdCheck generates a new cobra command for the viz extension.
func NewCmdCheck() *cobra.Command {
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

	cmd.Flags().StringVarP(&options.output, "output", "o", options.output, "Output format. One of: basic, json")
	cmd.Flags().BoolVar(&options.proxy, "proxy", options.proxy, "Also run data-plane checks, to determine if the data plane is healthy")
	cmd.Flags().DurationVar(&options.wait, "wait", options.wait, "Maximum allowed time for all tests to pass")
	cmd.Flags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace to use for --proxy checks (default: all namespaces)")
	return cmd
}

func configureAndRunChecks(wout io.Writer, werr io.Writer, options *checkOptions) error {
	err := options.validate()
	if err != nil {
		return fmt.Errorf("Validation error when executing check command: %v", err)
	}

	hc := vizHealthCheck.NewHealthChecker([]healthcheck.CategoryID{}, &healthcheck.Options{
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

	hc.AppendCategories(hc.VizCategory())
	if options.proxy {
		hc.AppendCategories(hc.VizDataPlaneCategory())
	}
	success := healthcheck.RunChecks(wout, werr, hc, options.output)

	if !success {
		os.Exit(1)
	}

	return nil
}
