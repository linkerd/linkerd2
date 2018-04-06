package cmd

import (
	"github.com/runconduit/conduit/controller/api/public"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var controlPlaneNamespace string
var apiAddr string // An empty value means "use the Kubernetes configuration"
var kubeconfigPath string
var verbose bool

var RootCmd = &cobra.Command{
	Use:   "conduit",
	Short: "conduit manages the Conduit service mesh",
	Long:  `conduit manages the Conduit service mesh.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// enable / disable logging
		if verbose {
			log.SetLevel(log.DebugLevel)
		} else {
			log.SetLevel(log.PanicLevel)
		}
	},
}

func init() {
	RootCmd.PersistentFlags().StringVarP(&controlPlaneNamespace, "conduit-namespace", "c", "conduit", "Namespace in which Conduit is installed")
	RootCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests")
	RootCmd.PersistentFlags().StringVar(&apiAddr, "api-addr", "", "Override kubeconfig and communicate directly with the control plane at host:port (mostly for testing)")
	RootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Turn on debug logging")
}

func newPublicAPIClient() (pb.ApiClient, error) {
	if apiAddr != "" {
		return public.NewInternalClient(apiAddr)
	}
	kubeAPI, err := k8s.NewAPI(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	return public.NewExternalClient(controlPlaneNamespace, kubeAPI)
}
