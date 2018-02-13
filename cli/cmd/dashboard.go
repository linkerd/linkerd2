package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/pkg/browser"
	healthcheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
	"github.com/runconduit/conduit/pkg/shell"
	"github.com/spf13/cobra"
)

var dashboardProxyPort int
var dashboardSkipBrowser bool

var dashboardCmd = &cobra.Command{
	Use:   "dashboard [flags]",
	Short: "Open the Conduit dashboard in a web browser",
	RunE: func(cmd *cobra.Command, args []string) error {
		if dashboardProxyPort < 0 {
			return fmt.Errorf("port must be greater than or equal to zero, was %d", dashboardProxyPort)
		}

		shellHomeDir := shell.NewUnixShell().HomeDir()
		kubernetesProxy, err := k8s.InitK8sProxy(shellHomeDir, kubeconfigPath, dashboardProxyPort)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to initialize proxy: %s\n", err)
			os.Exit(1)
		}

		url, err := kubernetesProxy.URLFor(controlPlaneNamespace, "/services/web:http/proxy/")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to generate URL for dashboard: %s\n", err)
			os.Exit(1)
		}

		client, err := newPublicAPIClient()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to initialize Conduit API client: %+v\n", err)
			os.Exit(1)
		}

		dashboardAvailable, err := isDashboardAvailable(client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed while checking availability of dashboard: %+v\n", err)
		}

		if !dashboardAvailable {
			fmt.Fprint(os.Stderr, "Conduit web deployment is not installed in your cluster\n")
			os.Exit(1)
		}

		fmt.Printf("Conduit dashboard available at:\n%s\n", url.String())

		if !dashboardSkipBrowser {
			fmt.Println("Opening the default browser")

			err = browser.OpenURL(url.String())
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to open URL %s in the default browser: %s", url, err)
				os.Exit(1)
			}
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

func init() {
	RootCmd.AddCommand(dashboardCmd)
	addControlPlaneNetworkingArgs(dashboardCmd)
	dashboardCmd.Args = cobra.NoArgs

	// This is identical to what `kubectl proxy --help` reports, `--port 0`
	// indicates a random port.
	dashboardCmd.PersistentFlags().IntVarP(&dashboardProxyPort, "port", "p", 8001, "The port on which to run the proxy. Set to 0 to pick a random port.")
	dashboardCmd.PersistentFlags().BoolVar(&dashboardSkipBrowser, "url", false, "Display the Conduit dashboard URL in the CLI instead of opening it in the default browser")
}
