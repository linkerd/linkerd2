package cmd

import (
	"fmt"
	"os"
	"regexp"

	"github.com/fatih/color"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	defaultLinkerdNamespace = "linkerd"
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

// NewCmdJaeger returns a new jeager command
func NewCmdJaeger() *cobra.Command {
	jaegerCmd := &cobra.Command{
		Use:   "jaeger",
		Short: "jaeger manages the jaeger extension of Linkerd service mesh",
		Long:  `jaeger manages the jaeger extension of Linkerd service mesh.`,
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

	jaegerCmd.PersistentFlags().StringVarP(&controlPlaneNamespace, "linkerd-namespace", "L", defaultLinkerdNamespace, "Namespace in which Linkerd is installed")
	jaegerCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests")
	jaegerCmd.PersistentFlags().StringVar(&kubeContext, "context", "", "Name of the kubeconfig context to use")
	jaegerCmd.PersistentFlags().StringVar(&impersonate, "as", "", "Username to impersonate for Kubernetes operations")
	jaegerCmd.PersistentFlags().StringArrayVar(&impersonateGroup, "as-group", []string{}, "Group to impersonate for Kubernetes operations")
	jaegerCmd.PersistentFlags().StringVar(&apiAddr, "api-addr", "", "Override kubeconfig and communicate directly with the control plane at host:port (mostly for testing)")
	jaegerCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Turn on debug logging")
	jaegerCmd.AddCommand(NewCmdCheck())
	jaegerCmd.AddCommand(newCmdDashboard())
	jaegerCmd.AddCommand(newCmdInstall())
	jaegerCmd.AddCommand(newCmdList())
	jaegerCmd.AddCommand(newCmdUninstall())

	return jaegerCmd
}

// checkForJaeger runs the kubernetesAPI, LinkerdControlPlaneExistence and the JaegerExtension category checks
// with a HealthChecker created by the passed options.
// For check failures, the process is exited with an error message based on the failed category.
func checkForJaeger(hcOptions healthcheck.Options) {
	checks := []healthcheck.CategoryID{
		healthcheck.KubernetesAPIChecks,
		healthcheck.LinkerdControlPlaneExistenceChecks,
		linkerdJaegerExtensionCheck,
	}

	hc := healthcheck.NewHealthChecker(checks, &hcOptions)
	hc.AppendCategories(jaegerCategory(hc))

	hc.RunChecks(exitOnError)
}

func exitOnError(result *healthcheck.CheckResult) {
	if result.Retry {
		fmt.Fprintln(os.Stderr, "Waiting for linkerd-jaeger to become available")
		return
	}

	if result.Err != nil && !result.Warning {
		var msg string
		switch result.Category {
		case healthcheck.KubernetesAPIChecks:
			msg = "Cannot connect to Kubernetes"
		case healthcheck.LinkerdControlPlaneExistenceChecks:
			msg = "Cannot find Linkerd"
		case linkerdJaegerExtensionCheck:
			msg = "Cannot find jaeger extension"
		}
		fmt.Fprintf(os.Stderr, "%s: %s\n", msg, result.Err)

		checkCmd := "linkerd jaeger check"
		fmt.Fprintf(os.Stderr, "Validate linkerd-jaeger install with: %s\n", checkCmd)

		os.Exit(1)
	}
}
