package cmd

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/linkerd/linkerd2/controller/gen/config"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	k8sResource "k8s.io/apimachinery/pkg/api/resource"
)

const (
	defaultNamespace = "linkerd"

	jsonOutput  = "json"
	tableOutput = "table"
	wideOutput  = "wide"
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
	apiAddr               string // An empty value means "use the Kubernetes configuration"
	kubeconfigPath        string
	kubeContext           string
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
		if controlPlaneNamespace == defaultNamespace && controlPlaneNamespaceFromEnv != "" {
			controlPlaneNamespace = controlPlaneNamespaceFromEnv
		}

		if !alphaNumDash.MatchString(controlPlaneNamespace) {
			return fmt.Errorf("%s is not a valid namespace", controlPlaneNamespace)
		}

		return nil
	},
}

func init() {
	RootCmd.PersistentFlags().StringVarP(&controlPlaneNamespace, "linkerd-namespace", "l", defaultNamespace, "Namespace in which Linkerd is installed [$LINKERD_NAMESPACE]")
	RootCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests")
	RootCmd.PersistentFlags().StringVar(&kubeContext, "context", "", "Name of the kubeconfig context to use")
	RootCmd.PersistentFlags().StringVar(&apiAddr, "api-addr", "", "Override kubeconfig and communicate directly with the control plane at host:port (mostly for testing)")
	RootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Turn on debug logging")

	RootCmd.AddCommand(newCmdCheck())
	RootCmd.AddCommand(newCmdCompletion())
	RootCmd.AddCommand(newCmdDashboard())
	RootCmd.AddCommand(newCmdDoc())
	RootCmd.AddCommand(newCmdEndpoints())
	RootCmd.AddCommand(newCmdGet())
	RootCmd.AddCommand(newCmdInject())
	RootCmd.AddCommand(newCmdInstall())
	RootCmd.AddCommand(newCmdInstallCNIPlugin())
	RootCmd.AddCommand(newCmdInstallSP())
	RootCmd.AddCommand(newCmdLogs())
	RootCmd.AddCommand(newCmdMetrics())
	RootCmd.AddCommand(newCmdProfile())
	RootCmd.AddCommand(newCmdRoutes())
	RootCmd.AddCommand(newCmdStat())
	RootCmd.AddCommand(newCmdTap())
	RootCmd.AddCommand(newCmdTop())
	RootCmd.AddCommand(newCmdUninject())
	RootCmd.AddCommand(newCmdVersion())
}

type statOptionsBase struct {
	namespace    string
	timeWindow   string
	outputFormat string
}

func newStatOptionsBase() *statOptionsBase {
	return &statOptionsBase{
		namespace:    "default",
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
		out = string(buffer.Bytes()[padding:])
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

// getPercentTLS calculates the percent of traffic that is TLS, from Public API
// BasicStats.
func getPercentTLS(stats *pb.BasicStats) float64 {
	reqTotal := stats.SuccessCount + stats.FailureCount
	if reqTotal == 0 {
		return 0.0
	}
	return float64(stats.TlsRequestCount) / float64(reqTotal)
}

// proxyConfigOptions holds values for command line flags that apply to both the
// install and inject commands. All fields in this struct should have
// corresponding flags added in the addProxyConfigFlags func later in this file.
type proxyConfigOptions struct {
	linkerdVersion          string
	proxyImage              string
	initImage               string
	dockerRegistry          string
	imagePullPolicy         string
	inboundPort             uint
	outboundPort            uint
	ignoreInboundPorts      []uint
	ignoreOutboundPorts     []uint
	proxyUID                int64
	proxyLogLevel           string
	proxyControlPort        uint
	proxyAdminPort          uint
	proxyCPURequest         string
	proxyMemoryRequest      string
	proxyCPULimit           string
	proxyMemoryLimit        string
	disableExternalProfiles bool
	noInitContainer         bool
}

const (
	defaultDockerRegistry = "gcr.io/linkerd-io"
)

// Deprecated. Use newConfig
func newProxyConfigOptions() *proxyConfigOptions {
	return &proxyConfigOptions{
		linkerdVersion:          version.Version,
		proxyImage:              defaultDockerRegistry + "/proxy",
		initImage:               defaultDockerRegistry + "/proxy-init",
		dockerRegistry:          defaultDockerRegistry,
		imagePullPolicy:         "IfNotPresent",
		inboundPort:             4143,
		outboundPort:            4140,
		ignoreInboundPorts:      nil,
		ignoreOutboundPorts:     nil,
		proxyUID:                2102,
		proxyLogLevel:           "warn,linkerd2_proxy=info",
		proxyControlPort:        4190,
		proxyAdminPort:          4191,
		proxyCPURequest:         "",
		proxyMemoryRequest:      "",
		proxyCPULimit:           "",
		proxyMemoryLimit:        "",
		disableExternalProfiles: false,
		noInitContainer:         false,
	}
}

func newConfig() configs {
	globalConfig := &config.Global{
		LinkerdNamespace: defaultNamespace,
		CniEnabled:       false,
		Version:          version.Version,
		IdentityContext:  nil,
	}
	proxyConfig := &config.Proxy{
		ProxyImage:              &config.Image{ImageName: defaultDockerRegistry + "/proxy", PullPolicy: "IfNotPresent"},
		ProxyInitImage:          &config.Image{ImageName: defaultDockerRegistry + "/proxy-init", PullPolicy: "IfNotPresent"},
		ControlPort:             &config.Port{Port: 4190},
		IgnoreInboundPorts:      nil,
		IgnoreOutboundPorts:     nil,
		InboundPort:             &config.Port{Port: 4143},
		AdminPort:               &config.Port{Port: 4191},
		OutboundPort:            &config.Port{Port: 4140},
		Resource:                &config.ResourceRequirements{RequestCpu: "", RequestMemory: "", LimitCpu: "", LimitMemory: ""},
		ProxyUid:                2102,
		LogLevel:                &config.LogLevel{Level: "warn,linkerd2_proxy=info"},
		DisableExternalProfiles: false,
	}
	return configs{globalConfig, proxyConfig}
}

func (options *proxyConfigOptions) validate() error {
	if !alphaNumDashDot.MatchString(options.linkerdVersion) {
		return fmt.Errorf("%s is not a valid version", options.linkerdVersion)
	}

	if !alphaNumDashDotSlashColon.MatchString(options.dockerRegistry) {
		return fmt.Errorf("%s is not a valid Docker registry. The url can contain only letters, numbers, dash, dot, slash and colon", options.dockerRegistry)
	}

	if options.imagePullPolicy != "Always" && options.imagePullPolicy != "IfNotPresent" && options.imagePullPolicy != "Never" {
		return fmt.Errorf("--image-pull-policy must be one of: Always, IfNotPresent, Never")
	}

	if options.proxyCPURequest != "" {
		if _, err := k8sResource.ParseQuantity(options.proxyCPURequest); err != nil {
			return fmt.Errorf("Invalid cpu request '%s' for --proxy-cpu-request flag", options.proxyCPURequest)
		}
	}

	if options.proxyMemoryRequest != "" {
		if _, err := k8sResource.ParseQuantity(options.proxyMemoryRequest); err != nil {
			return fmt.Errorf("Invalid memory request '%s' for --proxy-memory-request flag", options.proxyMemoryRequest)
		}
	}

	if options.proxyCPULimit != "" {
		cpuLimit, err := k8sResource.ParseQuantity(options.proxyCPULimit)
		if err != nil {
			return fmt.Errorf("Invalid cpu limit '%s' for --proxy-cpu-limit flag", options.proxyCPULimit)
		}
		if options.proxyCPURequest != "" {
			// Not checking for error because option proxyCPURequest was already validated
			if cpuRequest, _ := k8sResource.ParseQuantity(options.proxyCPURequest); cpuRequest.MilliValue() > cpuLimit.MilliValue() {
				return fmt.Errorf("The cpu limit '%s' cannot be lower than the cpu request '%s'", options.proxyCPULimit, options.proxyCPURequest)
			}
		}
	}

	if options.proxyMemoryLimit != "" {
		memoryLimit, err := k8sResource.ParseQuantity(options.proxyMemoryLimit)
		if err != nil {
			return fmt.Errorf("Invalid memory limit '%s' for --proxy-memory-limit flag", options.proxyMemoryLimit)
		}
		if options.proxyMemoryRequest != "" {
			// Not checking for error because option proxyMemoryRequest was already validated
			if memoryRequest, _ := k8sResource.ParseQuantity(options.proxyMemoryRequest); memoryRequest.Value() > memoryLimit.Value() {
				return fmt.Errorf("The memory limit '%s' cannot be lower than the memory request '%s'", options.proxyMemoryLimit, options.proxyMemoryRequest)
			}
		}
	}

	if !validProxyLogLevel.MatchString(options.proxyLogLevel) {
		return fmt.Errorf("\"%s\" is not a valid proxy log level - for allowed syntax check https://docs.rs/env_logger/0.6.0/env_logger/#enabling-logging",
			options.proxyLogLevel)
	}

	return nil
}

// registryOverride replaces the registry of the provided image if the image is
// using the default registry and the provided registry is not the default.
func registryOverride(image, registry string) string {
	return strings.Replace(image, defaultDockerRegistry, registry, 1)
}

// addProxyConfigFlags adds command line flags for all fields in the
// proxyConfigOptions struct. To keep things organized, the flags should be
// added in the order that they're defined in the proxyConfigOptions struct.
func addProxyConfigFlags(cmd *cobra.Command, options *proxyConfigOptions) {
	cmd.PersistentFlags().StringVarP(&options.linkerdVersion, "linkerd-version", "v", options.linkerdVersion, "Tag to be used for Linkerd images")
	cmd.PersistentFlags().StringVar(&options.proxyImage, "proxy-image", options.proxyImage, "Linkerd proxy container image name")
	cmd.PersistentFlags().StringVar(&options.initImage, "init-image", options.initImage, "Linkerd init container image name")
	cmd.PersistentFlags().StringVar(&options.dockerRegistry, "registry", options.dockerRegistry, "Docker registry to pull images from")
	cmd.PersistentFlags().StringVar(&options.imagePullPolicy, "image-pull-policy", options.imagePullPolicy, "Docker image pull policy")
	cmd.PersistentFlags().UintVar(&options.inboundPort, "inbound-port", options.inboundPort, "Proxy port to use for inbound traffic")
	cmd.PersistentFlags().UintVar(&options.outboundPort, "outbound-port", options.outboundPort, "Proxy port to use for outbound traffic")
	cmd.PersistentFlags().UintSliceVar(&options.ignoreInboundPorts, "skip-inbound-ports", options.ignoreInboundPorts, "Ports that should skip the proxy and send directly to the application")
	cmd.PersistentFlags().UintSliceVar(&options.ignoreOutboundPorts, "skip-outbound-ports", options.ignoreOutboundPorts, "Outbound ports that should skip the proxy")
	cmd.PersistentFlags().Int64Var(&options.proxyUID, "proxy-uid", options.proxyUID, "Run the proxy under this user ID")
	cmd.PersistentFlags().StringVar(&options.proxyLogLevel, "proxy-log-level", options.proxyLogLevel, "Log level for the proxy")
	cmd.PersistentFlags().UintVar(&options.proxyControlPort, "control-port", options.proxyControlPort, "Proxy port to use for control")
	cmd.PersistentFlags().UintVar(&options.proxyAdminPort, "admin-port", options.proxyAdminPort, "Proxy port to serve metrics on")
	cmd.PersistentFlags().StringVar(&options.proxyCPURequest, "proxy-cpu-request", options.proxyCPURequest, "Amount of CPU units that the proxy sidecar requests")
	cmd.PersistentFlags().StringVar(&options.proxyMemoryRequest, "proxy-memory-request", options.proxyMemoryRequest, "Amount of Memory that the proxy sidecar requests")
	cmd.PersistentFlags().StringVar(&options.proxyCPULimit, "proxy-cpu-limit", options.proxyCPULimit, "Maximum amount of CPU units that the proxy sidecar can use")
	cmd.PersistentFlags().StringVar(&options.proxyMemoryLimit, "proxy-memory-limit", options.proxyMemoryLimit, "Maximum amount of Memory that the proxy sidecar can use")
	cmd.PersistentFlags().BoolVar(&options.disableExternalProfiles, "disable-external-profiles", options.disableExternalProfiles, "Disables service profiles for non-Kubernetes services")
	cmd.PersistentFlags().BoolVar(&options.noInitContainer, "linkerd-cni-enabled", options.noInitContainer, "Experimental: Omit the proxy-init container when injecting the proxy; requires the linkerd-cni plugin to already be installed")

	// Deprecated flags
	cmd.PersistentFlags().StringVar(&options.proxyMemoryRequest, "proxy-memory", options.proxyMemoryRequest, "Amount of Memory that the proxy sidecar requests")
	cmd.PersistentFlags().StringVar(&options.proxyCPURequest, "proxy-cpu", options.proxyCPURequest, "Amount of CPU units that the proxy sidecar requests")

	cmd.PersistentFlags().MarkHidden("proxy-memory")
	cmd.PersistentFlags().MarkHidden("proxy-cpu")

	cmd.PersistentFlags().MarkDeprecated("proxy-memory", "use --proxy-memory-request instead")
	cmd.PersistentFlags().MarkDeprecated("proxy-cpu", "use --proxy-cpu-request instead")
}
