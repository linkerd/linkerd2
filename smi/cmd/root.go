package cmd

import (
	"fmt"
	"regexp"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	defaultLinkerdNamespace = "linkerd"

	smiExtensionName = "smi"
)

var (
	apiAddr               string // An empty value means "use the Kubernetes configuration"
	controlPlaneNamespace string
	kubeconfigPath        string
	kubeContext           string
	impersonate           string
	impersonateGroup      []string
	verbose               bool

	// These regexs are not as strict as they could be, but are a quick and dirty
	// sanity check against illegal characters.
	alphaNumDash = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)
)

// NewCmdSMI returns a new SMI command
func NewCmdSMI() *cobra.Command {
	smiCmd := &cobra.Command{
		Use:   "smi",
		Short: "smi manages the SMI extension of Linkerd service mesh",
		Long:  `smi manages the SMI extension of Linkerd service mesh.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// enable / disable logging
			if verbose {
				log.SetLevel(log.DebugLevel)
			} else {
				log.SetLevel(log.PanicLevel)
			}

			if !alphaNumDash.MatchString(controlPlaneNamespace) {
				return fmt.Errorf("%s is not a valid namespace", controlPlaneNamespace)
			}

			return nil
		},
	}

	smiCmd.PersistentFlags().StringVarP(&controlPlaneNamespace, "linkerd-namespace", "L", defaultLinkerdNamespace, "Namespace in which Linkerd is installed")
	smiCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests")
	smiCmd.PersistentFlags().StringVar(&kubeContext, "context", "", "Name of the kubeconfig context to use")
	smiCmd.PersistentFlags().StringVar(&impersonate, "as", "", "Username to impersonate for Kubernetes operations")
	smiCmd.PersistentFlags().StringArrayVar(&impersonateGroup, "as-group", []string{}, "Group to impersonate for Kubernetes operations")
	smiCmd.PersistentFlags().StringVar(&apiAddr, "api-addr", "", "Override kubeconfig and communicate directly with the control plane at host:port (mostly for testing)")
	smiCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Turn on debug logging")
	smiCmd.AddCommand(newCmdInstall())
	smiCmd.AddCommand(newCmdUninstall())

	return smiCmd
}
