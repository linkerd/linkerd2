package cmd

import (
	"fmt"
	"os"

	"github.com/pkg/browser"
	"github.com/runconduit/conduit/pkg/k8s"
	"github.com/runconduit/conduit/pkg/shell"
	"github.com/spf13/cobra"
)

var (
	proxyPort = -1
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard [flags]",
	Short: "Open the Conduit dashboard in a web browser",
	Long:  "Open the Conduit dashboard in a web browser.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if proxyPort < 0 {
			return fmt.Errorf("port must be greater than or equal to zero, was %d", proxyPort)
		}

		kp, err := k8s.InitK8sProxy(shell.NewUnixShell().HomeDir(), kubeconfigPath, proxyPort)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to initialize proxy: %s", err)
			os.Exit(1)
		}

		url, err := kp.URLFor(controlPlaneNamespace, "/services/web:http/proxy/")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to generate URL for dashboard: %s", err)
			os.Exit(1)
		}

		fmt.Printf("Opening [%s] in the default browser\n", url)
		err = browser.OpenURL(url.String())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open URL %s in the default browser: %s", url, err)
			os.Exit(1)
		}

		// blocks until killed
		err = kp.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error running proxy: %s", err)
			os.Exit(1)
		}

		return nil
	},
}

func init() {
	RootCmd.AddCommand(dashboardCmd)
	addControlPlaneNetworkingArgs(dashboardCmd)
	dashboardCmd.Args = cobra.NoArgs

	// This is identical to what `kubectl proxy --help` reports, `--port 0`
	// indicates a random port.
	dashboardCmd.PersistentFlags().IntVarP(&proxyPort, "port", "p", 8001, "The port on which to run the proxy. Set to 0 to pick a random port.")
}
