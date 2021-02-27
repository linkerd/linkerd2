package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/linkerd/linkerd2/cli/flag"
	jaeger "github.com/linkerd/linkerd2/jaeger/cmd"
	multicluster "github.com/linkerd/linkerd2/multicluster/cmd"
	viz "github.com/linkerd/linkerd2/viz/cmd"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	defaultLinkerdNamespace = "linkerd"
	defaultCNINamespace     = "linkerd-cni"
	defaultClusterDomain    = "cluster.local"
	defaultDockerRegistry   = "cr.l5d.io/linkerd"

	jsonOutput  = "json"
	tableOutput = "table"
)

var (
	// special handling for Windows, on all other platforms these resolve to
	// os.Stdout and os.Stderr, thanks to https://github.com/mattn/go-colorable
	stdout = color.Output
	stderr = color.Error

	okStatus   = color.New(color.FgGreen, color.Bold).SprintFunc()("\u221A")  // √
	warnStatus = color.New(color.FgYellow, color.Bold).SprintFunc()("\u203C") // ‼
	failStatus = color.New(color.FgRed, color.Bold).SprintFunc()("\u00D7")    // ×

	controlPlaneNamespace string
	cniNamespace          string
	apiAddr               string // An empty value means "use the Kubernetes configuration"
	kubeconfigPath        string
	kubeContext           string
	impersonate           string
	impersonateGroup      []string
	verbose               bool

	// These regexs are not as strict as they could be, but are a quick and dirty
	// sanity check against illegal characters.
	alphaNumDash              = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)
	alphaNumDashDot           = regexp.MustCompile(`^[\.a-zA-Z0-9-]+$`)
	alphaNumDashDotSlashColon = regexp.MustCompile(`^[\./a-zA-Z0-9-:]+$`)

	// Full Rust log level syntax at
	// https://docs.rs/env_logger/0.6.0/env_logger/#enabling-logging
	r                  = strings.NewReplacer("\t", "", "\n", "")
	validProxyLogLevel = regexp.MustCompile(r.Replace(`
		^(
			(
				(trace|debug|warn|info|error)|
				(\w|::)+|
				((\w|::)+=(trace|debug|warn|info|error))
			)(?:,|$)
		)+$`))
)

// RootCmd represents the root Cobra command
var RootCmd = &cobra.Command{
	Use:   "linkerd",
	Short: "linkerd manages the Linkerd service mesh",
	Long:  `linkerd manages the Linkerd service mesh.`,
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
	RootCmd.PersistentFlags().StringVarP(&controlPlaneNamespace, "linkerd-namespace", "L", defaultLinkerdNamespace, "Namespace in which Linkerd is installed ($LINKERD_NAMESPACE)")
	RootCmd.PersistentFlags().StringVarP(&cniNamespace, "cni-namespace", "", defaultCNINamespace, "Namespace in which the Linkerd CNI plugin is installed")
	RootCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests")
	RootCmd.PersistentFlags().StringVar(&kubeContext, "context", "", "Name of the kubeconfig context to use")
	RootCmd.PersistentFlags().StringVar(&impersonate, "as", "", "Username to impersonate for Kubernetes operations")
	RootCmd.PersistentFlags().StringArrayVar(&impersonateGroup, "as-group", []string{}, "Group to impersonate for Kubernetes operations")
	RootCmd.PersistentFlags().StringVar(&apiAddr, "api-addr", "", "Override kubeconfig and communicate directly with the control plane at host:port (mostly for testing)")
	RootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Turn on debug logging")
	RootCmd.AddCommand(newCmdAlpha())
	RootCmd.AddCommand(newCmdCheck())
	RootCmd.AddCommand(newCmdCompletion())
	RootCmd.AddCommand(newCmdDiagnostics())
	RootCmd.AddCommand(newCmdDoc())
	RootCmd.AddCommand(newCmdIdentity())
	RootCmd.AddCommand(newCmdInject())
	RootCmd.AddCommand(newCmdInstall())
	RootCmd.AddCommand(newCmdInstallCNIPlugin())
	RootCmd.AddCommand(newCmdProfile())
	RootCmd.AddCommand(newCmdRepair())
	RootCmd.AddCommand(newCmdUninject())
	RootCmd.AddCommand(newCmdUpgrade())
	RootCmd.AddCommand(newCmdVersion())
	RootCmd.AddCommand(newCmdUninstall())

	// Extension Sub Commands
	RootCmd.AddCommand(jaeger.NewCmdJaeger())
	RootCmd.AddCommand(multicluster.NewCmdMulticluster())
	RootCmd.AddCommand(viz.NewCmdViz())

	// Viz Extension sub commands
	RootCmd.AddCommand(deprecateCmd(viz.NewCmdDashboard()))
	RootCmd.AddCommand(deprecateCmd(viz.NewCmdEdges()))
	RootCmd.AddCommand(deprecateCmd(viz.NewCmdRoutes()))
	RootCmd.AddCommand(deprecateCmd(viz.NewCmdStat()))
	RootCmd.AddCommand(deprecateCmd(viz.NewCmdTap()))
	RootCmd.AddCommand(deprecateCmd(viz.NewCmdTop()))
}

func deprecateCmd(cmd *cobra.Command) *cobra.Command {
	cmd.Deprecated = fmt.Sprintf("use instead 'linkerd viz %s'\n", cmd.Use)
	return cmd
}

// registryOverride replaces the registry-portion of the provided image with the provided registry.
func registryOverride(image, newRegistry string) string {
	if image == "" {
		return image
	}
	registry := newRegistry
	if registry != "" && !strings.HasSuffix(registry, slash) {
		registry += slash
	}
	imageName := image
	if strings.Contains(image, slash) {
		imageName = image[strings.LastIndex(image, slash)+1:]
	}
	return registry + imageName
}

func flattenFlags(flags ...[]flag.Flag) []flag.Flag {
	out := []flag.Flag{}
	for _, f := range flags {
		out = append(out, f...)
	}
	return out
}
