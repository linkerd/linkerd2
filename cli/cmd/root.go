package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"path/filepath"
	"runtime"

	"github.com/runconduit/conduit/controller/api/public"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var cfgFile string
var controlPlaneNamespace string
var apiAddr string // An empty value means "use the Kubernetes configuration"
var kubeconfigPath string

var RootCmd = &cobra.Command{
	Use:   "conduit",
	Short: "conduit manages the Conduit service mesh",
	Long:  `conduit manages the Conduit service mesh.`,
}

// Execute adds all child commands to the root command sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
}

func init() {
	RootCmd.PersistentFlags().StringVarP(&controlPlaneNamespace, "conduit-namespace", "n", "conduit", "namespace in which Conduit is installed")
}

// TODO: decide if we want to use viper

func addControlPlaneNetworkingArgs(cmd *cobra.Command) {
	// See https://github.com/kubernetes/client-go/blob/master/examples/out-of-cluster-client-configuration/main.go
	kubeconfigDefaultPath := ""
	var homeEnvVar string
	if runtime.GOOS == "windows" {
		homeEnvVar = "USERPROFILE"
	} else {
		homeEnvVar = "HOME"
	}
	homeDir := os.Getenv(homeEnvVar)
	if homeDir != "" {
		kubeconfigDefaultPath = filepath.Join(homeDir, ".kube", "config")
	}
	// Use the same argument name as `kubectl` (see the output of `kubectl options`).
	cmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", kubeconfigDefaultPath, "Path to the kubeconfig file to use for CLI requests")

	cmd.PersistentFlags().StringVar(&apiAddr, "api-addr", "", "Override kubeconfig and communicate directly with the control plane at host:port (mostly for testing)")
}

func newApiClient() (pb.ApiClient, error) {
	var serverURL *url.URL
	var transport http.RoundTripper

	if apiAddr != "" {
		// TODO: Standalone local testing should be done over HTTPS too.
		serverURL = &url.URL{
			Scheme: "http",
			Host:   apiAddr,
			Path:   "/",
		}
		transport = http.DefaultTransport
	} else {
		kubeConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, err
		}
		serverURLBase, err := url.Parse(kubeConfig.Host)
		if err != nil {
			return nil, errors.New(fmt.Sprintf("invalid host in kubernetes config: %s", kubeConfig.Host))
		}
		proxyURLRef := url.URL{
			Path: fmt.Sprintf("api/v1/namespaces/%s/services/http:api:http/proxy/", controlPlaneNamespace),
		}
		serverURL = serverURLBase.ResolveReference(&proxyURLRef)

		transport, err = rest.TransportFor(kubeConfig)
		if err != nil {
			return nil, err
		}
	}

	apiConfig := &public.Config{
		ServerURL: serverURL,
	}
	return public.NewClient(apiConfig, transport)
}

// Exit with non-zero exit status without printing the command line usage and
// without printing the error message.
//
// When a `RunE` command returns an error, Cobra will print the usage message
// so the `RunE` function needs to handle any non-usage errors itself without
// returning an error. `exitSilentlyOnError` can be used as the `Run` (not
// `RunE`) function to help with this.
//
// TODO: This is used by the `version` command now; it should be used by other commands too.
func exitSilentlyOnError(f func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		if err := f(cmd, args); err != nil {
			os.Exit(2) // Reserve 1 for usage errors.
		}
	}
}
