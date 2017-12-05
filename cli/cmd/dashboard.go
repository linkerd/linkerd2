package cmd

import (
	"bufio"
	"fmt"
	"log"
	"os/exec"

	"github.com/pkg/browser"
	"github.com/spf13/cobra"
)

var (
	proxyPort int32 = -1
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard [flags]",
	Short: "Open the Conduit dashboard in a web browser",
	Long:  "Open the Conduit dashboard in a web browser.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if proxyPort <= 0 {
			log.Fatalf("port must be positive, was %d", proxyPort)
		}

		portArg := fmt.Sprintf("--port=%d", proxyPort)

		kubectl := exec.Command("kubectl", "proxy", portArg)
		kubeCtlStdOut, err := kubectl.StdoutPipe()
		if err != nil {
			log.Fatalf("Failed to set up pipe for kubectl output: %v", err)
		}

		fmt.Printf("Running `kubectl proxy %s`\n", portArg)

		go func() {
			// Wait for `kubectl proxy` to output one line, which indicates that the proxy has been set up.
			kubeCtlStdOutLines := bufio.NewReader(kubeCtlStdOut)
			firstLine, err := kubeCtlStdOutLines.ReadString('\n')
			if err != nil {
				log.Fatalf("Failed to read output from kubectl proxy: %v", err)
			}
			fmt.Printf("%s", firstLine)

			// Use "127.0.0.1" instead of "localhost" in case "localhost" resolves to "[::1]" (IPv6) or another
			// unexpected address.
			url := fmt.Sprintf("http://127.0.0.1:%d/api/v1/namespaces/%s/services/web:http/proxy/", proxyPort, controlPlaneNamespace)

			fmt.Printf("Opening %v in the default browser\n", url)
			err = browser.OpenURL(url)
			if err != nil {
				log.Fatalf("failed to open URL %s in the default browser: %v", url, err)
			}
		}()

		err = kubectl.Run()
		if err != nil {
			log.Fatalf("Failed to run %v: %v", kubectl, err)
		}

		return nil
	},
}

func init() {
	RootCmd.AddCommand(dashboardCmd)
	dashboardCmd.Args = cobra.NoArgs

	// This is identical to what `kubectl proxy --help` reports, except
	// `kubectl proxy` allows `--port=0` to indicate a random port; That's
	// inconvenient to support so it isn't supported.
	dashboardCmd.PersistentFlags().Int32VarP(&proxyPort, "port", "p", 8001, "The port on which to run the proxy, which must not be 0.")
}
