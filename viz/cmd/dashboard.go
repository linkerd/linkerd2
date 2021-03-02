package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/viz/pkg/api"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"
)

// These constants are used by the `show` flag.
const (
	// showLinkerd opens the Linkerd dashboard in a web browser (default).
	showLinkerd = "linkerd"

	// showGrafana opens the Grafana dashboard in a web browser.
	showGrafana = "grafana"

	// showURL displays dashboard URLs without opening a browser.
	showURL = "url"

	// webDeployment is the name of the web deployment in cli/install/template.go
	webDeployment = "web"

	// webPort is the http port from the web pod spec in cli/install/template.go
	webPort = 8084

	// defaultHost is the default host used for port-forwarding via `linkerd dashboard`
	defaultHost = "localhost"

	// defaultPort is for port-forwarding via `linkerd dashboard`
	defaultPort = 50750
)

// dashboardOptions holds values for command line flags that apply to the dashboard
// command.
type dashboardOptions struct {
	host string
	port int
	show string
	wait time.Duration
}

// newDashboardOptions initializes dashboard options with default
// values for host, port, and which dashboard to show. Also, set
// max wait time duration for 300 seconds for the dashboard to
// become available
//
// These options may be overridden on the CLI at run-time
func newDashboardOptions() *dashboardOptions {
	return &dashboardOptions{
		host: defaultHost,
		port: defaultPort,
		show: showLinkerd,
		wait: 300 * time.Second,
	}
}

// NewCmdDashboard creates a new cobra command `dashboard` which contains commands for visualizing linkerd's dashboards.
// After validating flag values, it will use the Kubernetes API to portforward requests to the Grafana and Web Deployments
// until the process gets killed/canceled
func NewCmdDashboard() *cobra.Command {
	options := newDashboardOptions()

	cmd := &cobra.Command{
		Use:   "dashboard [flags]",
		Short: "Open the Linkerd dashboard in a web browser",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.port < 0 {
				return fmt.Errorf("port must be greater than or equal to zero, was %d", options.port)
			}

			if options.show != showLinkerd && options.show != showGrafana && options.show != showURL {
				return fmt.Errorf("unknown value for 'show' param, was: %s, must be one of: %s, %s, %s",
					options.show, showLinkerd, showGrafana, showURL)
			}

			// ensure we can connect to the viz API before starting the proxy
			api.CheckClientOrRetryOrExit(healthcheck.Options{
				ControlPlaneNamespace: controlPlaneNamespace,
				KubeConfig:            kubeconfigPath,
				Impersonate:           impersonate,
				ImpersonateGroup:      impersonateGroup,
				KubeContext:           kubeContext,
				APIAddr:               apiAddr,
				RetryDeadline:         time.Now().Add(options.wait),
			}, true)

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			vizNs, err := k8sAPI.GetNamespaceWithExtensionLabel(context.Background(), ExtensionName)
			if err != nil {
				return err
			}

			signals := make(chan os.Signal, 1)
			signal.Notify(signals, os.Interrupt)
			defer signal.Stop(signals)

			portforward, err := k8s.NewPortForward(
				cmd.Context(),
				k8sAPI,
				vizNs.Name,
				webDeployment,
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
				fmt.Fprintf(os.Stderr, "Error running port-forward: %s\nCheck for `linkerd dashboard` running in other terminal sessions, or use the `--port` flag.\n", err)
				os.Exit(1)
			}

			go func() {
				<-signals
				portforward.Stop()
			}()

			webURL := portforward.URLFor("")
			grafanaURL := portforward.URLFor("/grafana")

			fmt.Printf("Linkerd dashboard available at:\n%s\n", webURL)
			fmt.Printf("Grafana dashboard available at:\n%s\n", grafanaURL)

			switch options.show {
			case showLinkerd:
				fmt.Println("Opening Linkerd dashboard in the default browser")

				err = browser.OpenURL(webURL)
				if err != nil {
					fmt.Fprintln(os.Stderr, "Failed to open Linkerd dashboard automatically")
					fmt.Fprintf(os.Stderr, "Visit %s in your browser to view the dashboard\n", webURL)
				}
			case showGrafana:
				fmt.Println("Opening Grafana dashboard in the default browser")

				err = browser.OpenURL(grafanaURL)
				if err != nil {
					fmt.Fprintln(os.Stderr, "Failed to open Grafana dashboard automatically")
					fmt.Fprintf(os.Stderr, "Visit %s in your browser to view the dashboard\n", grafanaURL)
				}
			case showURL:
				// no-op, we already printed the URLs
			}

			<-portforward.GetStop()
			return nil
		},
	}

	// This is identical to what `kubectl proxy --help` reports, `--port 0` indicates a random port.
	cmd.PersistentFlags().StringVar(&options.host, "address", options.host, "The address at which to serve requests")
	cmd.PersistentFlags().IntVarP(&options.port, "port", "p", options.port, "The local port on which to serve requests (when set to 0, a random port will be used)")
	cmd.PersistentFlags().StringVar(&options.show, "show", options.show, "Open a dashboard in a browser or show URLs in the CLI (one of: linkerd, grafana, url)")
	cmd.PersistentFlags().DurationVar(&options.wait, "wait", options.wait, "Wait for dashboard to become available if it's not available when the command is run")

	return cmd
}
