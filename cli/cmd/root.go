package cmd

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/fatih/color"
	"github.com/linkerd/linkerd2/cli/flag"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	defaultLinkerdNamespace = "linkerd"
	defaultCNINamespace     = "linkerd-cni"
	defaultClusterDomain    = "cluster.local"
	defaultDockerRegistry   = "ghcr.io/linkerd"

	jsonOutput  = "json"
	tableOutput = "table"
	wideOutput  = "wide"

	maxRps = 100.0
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
	defaultNamespace      string // Default namespace taken from current kubectl context
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
	defaultNamespace = getDefaultNamespace()
	RootCmd.PersistentFlags().StringVarP(&controlPlaneNamespace, "linkerd-namespace", "L", defaultLinkerdNamespace, "Namespace in which Linkerd is installed [$LINKERD_NAMESPACE]")
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
	RootCmd.AddCommand(newCmdDashboard())
	RootCmd.AddCommand(newCmdDiagnostics())
	RootCmd.AddCommand(newCmdDoc())
	RootCmd.AddCommand(newCmdEdges())
	RootCmd.AddCommand(newCmdEndpoints())
	RootCmd.AddCommand(newCmdInject())
	RootCmd.AddCommand(newCmdInstall())
	RootCmd.AddCommand(newCmdInstallCNIPlugin())
	RootCmd.AddCommand(newCmdInstallSP())
	RootCmd.AddCommand(newCmdMetrics())
	RootCmd.AddCommand(newCmdProfile())
	RootCmd.AddCommand(newCmdRepair())
	RootCmd.AddCommand(newCmdRoutes())
	RootCmd.AddCommand(newCmdStat())
	RootCmd.AddCommand(newCmdTap())
	RootCmd.AddCommand(newCmdTop())
	RootCmd.AddCommand(newCmdUninject())
	RootCmd.AddCommand(newCmdUpgrade())
	RootCmd.AddCommand(newCmdVersion())
	RootCmd.AddCommand(newCmdMulticluster())
	RootCmd.AddCommand(newCmdUninstall())
}

type statOptionsBase struct {
	namespace    string
	timeWindow   string
	outputFormat string
}

func newStatOptionsBase() *statOptionsBase {
	return &statOptionsBase{
		namespace:    defaultNamespace,
		timeWindow:   "1m",
		outputFormat: tableOutput,
	}
}

func (o *statOptionsBase) validateOutputFormat() error {
	switch o.outputFormat {
	case tableOutput, jsonOutput, wideOutput:
		return nil
	default:
		return fmt.Errorf("--output currently only supports %s, %s and %s", tableOutput, jsonOutput, wideOutput)
	}
}

func renderStats(buffer bytes.Buffer, options *statOptionsBase) string {
	var out string
	switch options.outputFormat {
	case jsonOutput:
		out = buffer.String()
	default:
		// strip left padding on the first column
		b := buffer.Bytes()
		if len(b) > padding {
			out = string(b[padding:])
		}
		out = strings.Replace(out, "\n"+strings.Repeat(" ", padding), "\n", -1)
	}

	return out
}

// getRequestRate calculates request rate from Public API BasicStats.
func getRequestRate(success, failure uint64, timeWindow string) float64 {
	windowLength, err := time.ParseDuration(timeWindow)
	if err != nil {
		log.Error(err.Error())
		return 0.0
	}
	return float64(success+failure) / windowLength.Seconds()
}

// getSuccessRate calculates success rate from Public API BasicStats.
func getSuccessRate(success, failure uint64) float64 {
	if success+failure == 0 {
		return 0.0
	}
	return float64(success) / float64(success+failure)
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
