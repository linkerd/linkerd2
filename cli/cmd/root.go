package cmd

import (
	"os"

	"github.com/runconduit/conduit/controller/api/public"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
	"github.com/runconduit/conduit/pkg/shell"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var cfgFile string
var controlPlaneNamespace string
var apiAddr string // An empty value means "use the Kubernetes configuration"
var kubeconfigPath string
var logLevel string

var RootCmd = &cobra.Command{
	Use:   "conduit",
	Short: "conduit manages the Conduit service mesh",
	Long:  `conduit manages the Conduit service mesh.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// set global log level
		level, err := log.ParseLevel(logLevel)
		if err != nil {
			log.Fatalf("invalid log-level: %s", logLevel)
		}
		log.SetLevel(level)
	},
}

func init() {
	RootCmd.PersistentFlags().StringVarP(&controlPlaneNamespace, "conduit-namespace", "n", "conduit", "namespace in which Conduit is installed")
	RootCmd.PersistentFlags().StringVar(&logLevel, "log-level", log.FatalLevel.String(), "log level, must be one of: panic, fatal, error, warn, info, debug")
}

// TODO: decide if we want to use viper

func addControlPlaneNetworkingArgs(cmd *cobra.Command) {
	// Use the same argument name as `kubectl` (see the output of `kubectl options`).
	//TODO: move these to init() as they are globally applicable
	cmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests")
	cmd.PersistentFlags().StringVar(&apiAddr, "api-addr", "", "Override kubeconfig and communicate directly with the control plane at host:port (mostly for testing)")
}

func newPublicAPIClient() (pb.ApiClient, error) {
	if apiAddr != "" {
		return public.NewInternalClient(apiAddr)
	}
	kubeApi, err := k8s.NewK8sAPI(shell.NewUnixShell(), kubeconfigPath)
	if err != nil {
		return nil, err
	}
	return public.NewExternalClient(controlPlaneNamespace, kubeApi)
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
