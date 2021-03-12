package cmd

import (
	"fmt"
	"regexp"

	"github.com/fatih/color"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	// ExtensionName is the value that the viz extension resources should be labeled with
	ExtensionName = "viz"

	vizChartName            = "linkerd-viz"
	defaultLinkerdNamespace = "linkerd"
	maxRps                  = 100.0

	jsonOutput  = "json"
	tableOutput = "table"
	wideOutput  = "wide"
)

var (
	// special handling for Windows, on all other platforms these resolve to
	// os.Stdout and os.Stderr, thanks to https://github.com/mattn/go-colorable
	stdout = color.Output
	stderr = color.Error

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

// NewCmdViz returns a new jeager command
func NewCmdViz() *cobra.Command {
	vizCmd := &cobra.Command{
		Use:   "viz",
		Short: "viz manages the linkerd-viz extension of Linkerd service mesh",
		Long:  `viz manages the linkerd-viz extension of Linkerd service mesh.`,
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

	vizCmd.PersistentFlags().StringVarP(&controlPlaneNamespace, "linkerd-namespace", "L", defaultLinkerdNamespace, "Namespace in which Linkerd is installed")
	vizCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests")
	vizCmd.PersistentFlags().StringVar(&kubeContext, "context", "", "Name of the kubeconfig context to use")
	vizCmd.PersistentFlags().StringVar(&impersonate, "as", "", "Username to impersonate for Kubernetes operations")
	vizCmd.PersistentFlags().StringArrayVar(&impersonateGroup, "as-group", []string{}, "Group to impersonate for Kubernetes operations")
	vizCmd.PersistentFlags().StringVar(&apiAddr, "api-addr", "", "Override kubeconfig and communicate directly with the control plane at host:port (mostly for testing)")
	vizCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Turn on debug logging")
	vizCmd.AddCommand(NewCmdCheck())
	vizCmd.AddCommand(NewCmdDashboard())
	vizCmd.AddCommand(NewCmdEdges())
	vizCmd.AddCommand(newCmdInstall())
	vizCmd.AddCommand(newCmdList())
	vizCmd.AddCommand(newCmdProfile())
	vizCmd.AddCommand(NewCmdRoutes())
	vizCmd.AddCommand(NewCmdStat())
	vizCmd.AddCommand(NewCmdTap())
	vizCmd.AddCommand(NewCmdTop())
	vizCmd.AddCommand(newCmdUninstall())

	return vizCmd
}
