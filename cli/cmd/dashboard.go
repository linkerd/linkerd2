package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
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
)

type dashboardOptions struct {
	dashboardProxyPort int
	dashboardShow      string
	wait               time.Duration
}

func newDashboardOptions() *dashboardOptions {
	return &dashboardOptions{
		dashboardProxyPort: 0,
		dashboardShow:      showLinkerd,
		wait:               300 * time.Second,
	}
}

func newCmdDashboard() *cobra.Command {
	options := newDashboardOptions()

	cmd := &cobra.Command{
		Use:   "dashboard [flags]",
		Short: "Open the Linkerd dashboard in a web browser",
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.dashboardProxyPort < 0 {
				return fmt.Errorf("port must be greater than or equal to zero, was %d", options.dashboardProxyPort)
			}

			if options.dashboardShow != showLinkerd && options.dashboardShow != showGrafana && options.dashboardShow != showURL {
				return fmt.Errorf("unknown value for 'show' param, was: %s, must be one of: %s, %s, %s",
					options.dashboardShow, showLinkerd, showGrafana, showURL)
			}

			kubernetesProxy, err := k8s.NewProxy(kubeconfigPath, kubeContext, options.dashboardProxyPort)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to initialize proxy: %s\n", err)
				os.Exit(1)
			}

			url, err := kubernetesProxy.URLFor(controlPlaneNamespace, "/services/linkerd-web:http/proxy/")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to generate URL for dashboard: %s\n", err)
				os.Exit(1)
			}

			grafanaURL, err := kubernetesProxy.URLFor(controlPlaneNamespace, "/services/linkerd-grafana:http/proxy/")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to generate URL for Grafana: %s\n", err)
				os.Exit(1)
			}

			// ensure we can connect to the public API before starting the proxy
			validatedPublicAPIClient(time.Now().Add(options.wait))

			fmt.Printf("Linkerd dashboard available at:\n%s\n", url.String())
			fmt.Printf("Grafana dashboard available at:\n%s\n", grafanaURL.String())

			switch options.dashboardShow {
			case showLinkerd:
				fmt.Println("Opening Linkerd dashboard in the default browser")

				err = browser.OpenURL(url.String())
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to open Linkerd URL %s in the default browser: %s", url, err)
					os.Exit(1)
				}
			case showGrafana:
				fmt.Println("Opening Grafana dashboard in the default browser")

				err = browser.OpenURL(grafanaURL.String())
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to open Grafana URL %s in the default browser: %s", grafanaURL, err)
					os.Exit(1)
				}
			case showURL:
				// no-op, we already printed the URLs
			}

			// blocks until killed
			err = kubernetesProxy.Run()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error running proxy: %s", err)
				os.Exit(1)
			}

			return nil
		},
	}

	cmd.Args = cobra.NoArgs
	// This is identical to what `kubectl proxy --help` reports, `--port 0` indicates a random port.
	cmd.PersistentFlags().IntVarP(&options.dashboardProxyPort, "port", "p", options.dashboardProxyPort, "The port on which to run the proxy (when set to 0, a random port will be used)")
	cmd.PersistentFlags().StringVar(&options.dashboardShow, "show", options.dashboardShow, "Open a dashboard in a browser or show URLs in the CLI (one of: linkerd, grafana, url)")
	cmd.PersistentFlags().DurationVar(&options.wait, "wait", options.wait, "Wait for dashboard to become available if it's not available when the command is run")

	return cmd
}
