package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/linkerd/linkerd2/cli/flag"
	multicluster "github.com/linkerd/linkerd2/multicluster/cmd"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/inject"
	viz "github.com/linkerd/linkerd2/viz/cmd"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	defaultLinkerdNamespace = "linkerd"
	defaultCNINamespace     = "linkerd-cni"
	defaultClusterDomain    = "cluster.local"

	jsonOutput  = pkgcmd.JsonOutput
	yamlOutput  = pkgcmd.YamlOutput
	tableOutput = healthcheck.TableOutput
	shortOutput = healthcheck.ShortOutput
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
func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
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

			controlPlaneNamespaceFromEnv := os.Getenv(flags.EnvOverrideNamespace)
			if controlPlaneNamespace == defaultLinkerdNamespace && controlPlaneNamespaceFromEnv != "" {
				controlPlaneNamespace = controlPlaneNamespaceFromEnv
			}

			if !alphaNumDash.MatchString(controlPlaneNamespace) {
				return fmt.Errorf("%s is not a valid namespace", controlPlaneNamespace)
			}

			return nil
		},
	}

	rootCmd.PersistentFlags().StringVarP(&controlPlaneNamespace, "linkerd-namespace", "L",
		defaultLinkerdNamespace,
		fmt.Sprintf("Namespace in which Linkerd is installed ($%s)", flags.EnvOverrideNamespace))
	rootCmd.PersistentFlags().StringVarP(&cniNamespace, "cni-namespace", "", defaultCNINamespace, "Namespace in which the Linkerd CNI plugin is installed")
	rootCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests")
	rootCmd.PersistentFlags().StringVar(&kubeContext, "context", "", "Name of the kubeconfig context to use")
	rootCmd.PersistentFlags().StringVar(&impersonate, "as", "", "Username to impersonate for Kubernetes operations")
	rootCmd.PersistentFlags().StringArrayVar(&impersonateGroup, "as-group", []string{}, "Group to impersonate for Kubernetes operations")
	rootCmd.PersistentFlags().StringVar(&apiAddr, "api-addr", "", "Override kubeconfig and communicate directly with the control plane at host:port (mostly for testing)")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Turn on debug logging")
	rootCmd.AddCommand(newCmdCheck())
	rootCmd.AddCommand(newCmdCompletion())
	rootCmd.AddCommand(newCmdDiagnostics())
	rootCmd.AddCommand(newCmdDoc(rootCmd))
	rootCmd.AddCommand(newCmdIdentity())
	rootCmd.AddCommand(NewCmdInject(inject.GetOverriddenValues))
	rootCmd.AddCommand(newCmdInstall())
	rootCmd.AddCommand(newCmdInstallCNIPlugin())
	rootCmd.AddCommand(newCmdProfile())
	rootCmd.AddCommand(newCmdAuthz())
	rootCmd.AddCommand(newCmdUninject())
	rootCmd.AddCommand(newCmdUpgrade())
	rootCmd.AddCommand(newCmdVersion())
	rootCmd.AddCommand(newCmdUninstall())
	rootCmd.AddCommand(newCmdPrune())

	// Extension Sub Commands
	rootCmd.AddCommand(multicluster.NewCmdMulticluster())
	rootCmd.AddCommand(viz.NewCmdViz())

	// Viz Extension sub commands
	rootCmd.AddCommand(deprecateCmd(viz.NewCmdDashboard()))
	rootCmd.AddCommand(deprecateCmd(viz.NewCmdEdges()))
	rootCmd.AddCommand(deprecateCmd(viz.NewCmdRoutes()))
	rootCmd.AddCommand(deprecateCmd(viz.NewCmdStat()))
	rootCmd.AddCommand(deprecateCmd(viz.NewCmdTap()))
	rootCmd.AddCommand(deprecateCmd(viz.NewCmdTop()))

	// resource-aware completion flag configurations
	pkgcmd.ConfigureNamespaceFlagCompletion(
		rootCmd, []string{"linkerd-namespace", "cni-namespace"},
		kubeconfigPath, impersonate, impersonateGroup, kubeContext)

	pkgcmd.ConfigureKubeContextFlagCompletion(rootCmd, kubeconfigPath)

	return rootCmd
}

func deprecateCmd(cmd *cobra.Command) *cobra.Command {
	cmd.Deprecated = fmt.Sprintf("use instead 'linkerd viz %s'\n", cmd.Use)
	return cmd
}

func flattenFlags(flags ...[]flag.Flag) []flag.Flag {
	out := []flag.Flag{}
	for _, f := range flags {
		out = append(out, f...)
	}
	return out
}
