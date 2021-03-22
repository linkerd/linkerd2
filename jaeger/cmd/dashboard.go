package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"
)

const (

	// jaegerDeployment is the name of the jaeger deployment
	jaegerDeployment = "jaeger"

	// webPort is the http port of the jaeger deployment
	webPort = 16686

	// defaultHost is the default host used for port-forwarding via `jaeger dashboard`
	defaultHost = "localhost"

	// defaultPort is for port-forwarding via `jaeger dashboard`
	defaultPort = 16686
)

// dashboardOptions holds values for command line flags that apply to the dashboard
// command.
type dashboardOptions struct {
	host    string
	port    int
	showURL bool
	wait    time.Duration
}

// newDashboardOptions initializes dashboard options with default
// values for host, port. Also, set max wait time duration for
// 300 seconds for the dashboard to become available
//
// These options may be overridden on the CLI at run-time
func newDashboardOptions() *dashboardOptions {
	return &dashboardOptions{
		host: defaultHost,
		port: defaultPort,
		wait: 300 * time.Second,
	}
}

// newCmdDashboard creates a new cobra command `dashboard` which contains commands for visualizing jaeger extension's dashboards.
// After validating flag values, it will use the Kubernetes API to portforward requests to the Jaeger deployment
// until the process gets killed/canceled
func newCmdDashboard() *cobra.Command {
	options := newDashboardOptions()

	cmd := &cobra.Command{
		Use:   "dashboard [flags]",
		Short: "Open the Jaeger extension dashboard in a web browser",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.port < 0 {
				return fmt.Errorf("port must be greater than or equal to zero, was %d", options.port)
			}

			checkForJaeger(healthcheck.Options{
				ControlPlaneNamespace: controlPlaneNamespace,
				KubeConfig:            kubeconfigPath,
				Impersonate:           impersonate,
				ImpersonateGroup:      impersonateGroup,
				KubeContext:           kubeContext,
				APIAddr:               apiAddr,
				RetryDeadline:         time.Now().Add(options.wait),
			})

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			jaegerNamespace, err := k8sAPI.GetNamespaceWithExtensionLabel(cmd.Context(), JaegerExtensionName)
			if err != nil {
				return err
			}

			signals := make(chan os.Signal, 1)
			signal.Notify(signals, os.Interrupt)
			defer signal.Stop(signals)

			portforward, err := k8s.NewPortForward(
				cmd.Context(),
				k8sAPI,
				jaegerNamespace.Name,
				jaegerDeployment,
				options.host,
				options.port,
				webPort,
				verbose,
			)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to initialize port-forward: %s\n", err)
				os.Exit(1)
			}

			if err = portforward.Init(); err != nil {
				// TODO: consider falling back to an ephemeral port if defaultPort is taken
				fmt.Fprintf(os.Stderr, "Error running port-forward: %s\nCheck for `jaeger dashboard` running in other terminal sessions, or use the `--port` flag.\n", err)
				os.Exit(1)
			}

			go func() {
				<-signals
				portforward.Stop()
			}()

			webURL := portforward.URLFor("")

			fmt.Printf("Jaeger extension dashboard available at:\n%s\n", webURL)

			if !options.showURL {
				err = browser.OpenURL(webURL)
				if err != nil {
					fmt.Fprintln(os.Stderr, "Failed to open dashboard automatically")
					fmt.Fprintf(os.Stderr, "Visit %s in your browser to view the dashboard\n", webURL)
				}
			}

			<-portforward.GetStop()
			return nil
		},
	}

	// This is identical to what `kubectl proxy --help` reports, `--port 0` indicates a random port.
	cmd.PersistentFlags().StringVar(&options.host, "address", options.host, "The address at which to serve requests")
	cmd.PersistentFlags().IntVarP(&options.port, "port", "p", options.port, "The local port on which to serve requests (when set to 0, a random port will be used)")
	cmd.PersistentFlags().BoolVar(&options.showURL, "show-url", options.showURL, "show only URL in the CLI, and do not open the browser")
	cmd.PersistentFlags().DurationVar(&options.wait, "wait", options.wait, "Wait for dashboard to become available if it's not available when the command is run")

	return cmd
}
