package cmd

import (
	"fmt"
	"os"
	"regexp"

	"github.com/fatih/color"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	defaultLinkerdNamespace = "linkerd"
)

var (
	// special handling for Windows, on all other platforms these resolve to
	// os.Stdout and os.Stderr, thanks to https://github.com/mattn/go-colorable
	stdout = color.Output
	stderr = color.Error

	// These regexs are not as strict as they could be, but are a quick and dirty
	// sanity check against illegal characters.
	alphaNumDash = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

	controlPlaneNamespace string
	apiAddr               string // An empty value means "use the Kubernetes configuration"
	kubeconfigPath        string
	kubeContext           string
	defaultNamespace      string // Default namespace taken from current kubectl context
	impersonate           string
	impersonateGroup      []string
	verbose               bool
)

// RootCmd represents the root Cobra command
var RootCmd = &cobra.Command{
	Use:   "jaeger",
	Short: "jaeger manages the jaeger extension of Linkerd service mesh",
	Long:  `linkerd manages the  jaeger extension of Linkerd service mesh.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// enable / disable logging
		if verbose {
			log.SetLevel(log.DebugLevel)
		} else {
			log.SetLevel(log.PanicLevel)
		}

		controlPlaneNamespaceFromEnv := os.Getenv("LINKERD_NAMESPACE")
		if controlPlaneNamespace == defaultLinkerdNamespace && controlPlaneNamespaceFromEnv != "" {
			controlPlaneNamespace = controlPlaneNamespaceFromEnv
		}

		if !alphaNumDash.MatchString(controlPlaneNamespace) {
			return fmt.Errorf("%s is not a valid namespace", controlPlaneNamespace)
		}

		return nil
	},
}

func init() {
	defaultNamespace = getDefaultNamespace()
	RootCmd.PersistentFlags().StringVarP(&controlPlaneNamespace, "linkerd-namespace", "L", defaultLinkerdNamespace, "Namespace in which Linkerd is installed [$LINKERD_NAMESPACE]")
	RootCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests")
	RootCmd.PersistentFlags().StringVar(&kubeContext, "context", "", "Name of the kubeconfig context to use")
	RootCmd.PersistentFlags().StringVar(&impersonate, "as", "", "Username to impersonate for Kubernetes operations")
	RootCmd.PersistentFlags().StringArrayVar(&impersonateGroup, "as-group", []string{}, "Group to impersonate for Kubernetes operations")
	RootCmd.PersistentFlags().StringVar(&apiAddr, "api-addr", "", "Override kubeconfig and communicate directly with the control plane at host:port (mostly for testing)")
	RootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Turn on debug logging")
	RootCmd.AddCommand(newCmdInstall())
}

// getDefaultNamespace fetches the default namespace
// used in the current KubeConfig context
func getDefaultNamespace() string {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()

	if kubeconfigPath != "" {
		rules.ExplicitPath = kubeconfigPath
	}

	overrides := &clientcmd.ConfigOverrides{CurrentContext: kubeContext}
	kubeCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)
	ns, _, err := kubeCfg.Namespace()

	if err != nil {
		log.Warnf(`could not set namespace from kubectl context, using 'default' namespace: %s
		 ensure the KUBECONFIG path %s is valid`, err, kubeconfigPath)
		return corev1.NamespaceDefault
	}

	return ns
}
