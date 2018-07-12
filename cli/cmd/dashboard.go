package cmd

import (
	"context"
	"fmt"
	"os"

	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/pkg/browser"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// These constants are used by the `show` flag.
const (
	// showConduit opens the Conduit dashboard in a web browser (default).
	showConduit = "conduit"

	// showGrafana opens the Grafana dashboard in a web browser.
	showGrafana = "grafana"

	// showURL displays dashboard URLs without opening a browser.
	showURL = "url"
)

type dashboardOptions struct {
	dashboardProxyPort int
	dashboardShow      string
}

func newDashboardOptions() *dashboardOptions {
	return &dashboardOptions{
		dashboardProxyPort: 0,
		dashboardShow:      "conduit",
	}
}

func newCmdDashboard() *cobra.Command {
	options := newDashboardOptions()

	cmd := &cobra.Command{
		Use:   "dashboard [flags]",
		Short: "Open the Conduit dashboard in a web browser",
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.dashboardProxyPort < 0 {
				return fmt.Errorf("port must be greater than or equal to zero, was %d", options.dashboardProxyPort)
			}

			if options.dashboardShow != showConduit && options.dashboardShow != showGrafana && options.dashboardShow != showURL {
				return fmt.Errorf("unknown value for 'show' param, was: %s, must be one of: %s, %s, %s",
					options.dashboardShow, showConduit, showGrafana, showURL)
			}

			kubernetesProxy, err := k8s.NewProxy(kubeconfigPath, options.dashboardProxyPort)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to initialize proxy: %s\n", err)
				os.Exit(1)
			}

			url, err := kubernetesProxy.URLFor(controlPlaneNamespace, "/services/web:http/proxy/")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to generate URL for dashboard: %s\n", err)
				os.Exit(1)
			}

			grafanaUrl, err := kubernetesProxy.URLFor(controlPlaneNamespace, "/services/grafana:http/proxy/")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to generate URL for Grafana: %s\n", err)
				os.Exit(1)
			}

			client, err := newPublicAPIClient()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to initialize Conduit API client: %+v\n", err)
				os.Exit(1)
			}

			dashboardAvailable, err := isDashboardAvailable(client)
			if err != nil {
				log.Debugf("Error checking dashboard availability: %s", err)
			}

			if err != nil || !dashboardAvailable {
				fmt.Fprintf(os.Stderr, "Conduit is not running in the \"%s\" namespace\n", controlPlaneNamespace)
				fmt.Fprintf(os.Stderr, "Install with: conduit install --linkerd-namespace %s | kubectl apply -f -\n", controlPlaneNamespace)
				os.Exit(1)
			}

			fmt.Printf("Conduit dashboard available at:\n%s\n", url.String())
			fmt.Printf("Grafana dashboard available at:\n%s\n", grafanaUrl.String())

			switch options.dashboardShow {
			case showConduit:
				fmt.Println("Opening Conduit dashboard in the default browser")

				err = browser.OpenURL(url.String())
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to open Conduit URL %s in the default browser: %s", url, err)
					os.Exit(1)
				}
			case showGrafana:
				fmt.Println("Opening Grafana dashboard in the default browser")

				err = browser.OpenURL(grafanaUrl.String())
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to open Grafana URL %s in the default browser: %s", grafanaUrl, err)
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
	cmd.PersistentFlags().StringVar(&options.dashboardShow, "show", options.dashboardShow, "Open a dashboard in a browser or show URLs in the CLI (one of: conduit, grafana, url)")

	return cmd
}

func isDashboardAvailable(client pb.ApiClient) (bool, error) {
	res, err := client.SelfCheck(context.Background(), &healthcheckPb.SelfCheckRequest{})
	if err != nil {
		return false, err
	}

	for _, result := range res.Results {
		if result.Status != healthcheckPb.CheckStatus_OK {
			return false, nil
		}
	}
	return true, nil
}
