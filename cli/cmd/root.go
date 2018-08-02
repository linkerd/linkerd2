package cmd

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/controller/api/public"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var controlPlaneNamespace string
var apiAddr string // An empty value means "use the Kubernetes configuration"
var kubeconfigPath string
var verbose bool

var (
	// These regexs are not as strict as they could be, but are a quick and dirty
	// sanity check against illegal characters.
	alphaNumDash         = regexp.MustCompile("^[a-zA-Z0-9-]+$")
	alphaNumDashDot      = regexp.MustCompile("^[\\.a-zA-Z0-9-]+$")
	alphaNumDashDotSlash = regexp.MustCompile("^[\\./a-zA-Z0-9-]+$")
)

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

		if !alphaNumDash.MatchString(controlPlaneNamespace) {
			return fmt.Errorf("%s is not a valid namespace", controlPlaneNamespace)
		}

		return nil
	},
}

func init() {
	RootCmd.PersistentFlags().StringVarP(&controlPlaneNamespace, "linkerd-namespace", "l", "linkerd", "Namespace in which Linkerd is installed")
	RootCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests")
	RootCmd.PersistentFlags().StringVar(&apiAddr, "api-addr", "", "Override kubeconfig and communicate directly with the control plane at host:port (mostly for testing)")
	RootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Turn on debug logging")

	RootCmd.AddCommand(newCmdCheck())
	RootCmd.AddCommand(newCmdCompletion())
	RootCmd.AddCommand(newCmdDashboard())
	RootCmd.AddCommand(newCmdGet())
	RootCmd.AddCommand(newCmdInject())
	RootCmd.AddCommand(newCmdInstall())
	RootCmd.AddCommand(newCmdStat())
	RootCmd.AddCommand(newCmdTap())
	RootCmd.AddCommand(newCmdVersion())
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

type proxyConfigOptions struct {
	linkerdVersion        string
	proxyImage            string
	initImage             string
	dockerRegistry        string
	imagePullPolicy       string
	proxyUID              int64
	proxyLogLevel         string
	proxyBindTimeout      string
	proxyAPIPort          uint
	proxyControlPort      uint
	proxyMetricsPort      uint
	proxyOutboundCapacity map[string]uint
	tls                   string
}

const (
	optionalTLS           = "optional"
	defaultDockerRegistry = "gcr.io/linkerd-io"
)

func newProxyConfigOptions() *proxyConfigOptions {
	return &proxyConfigOptions{
		linkerdVersion:        version.Version,
		proxyImage:            defaultDockerRegistry + "/proxy",
		initImage:             defaultDockerRegistry + "/proxy-init",
		dockerRegistry:        defaultDockerRegistry,
		imagePullPolicy:       "IfNotPresent",
		proxyUID:              2102,
		proxyLogLevel:         "warn,linkerd2_proxy=info",
		proxyBindTimeout:      "10s",
		proxyAPIPort:          8086,
		proxyControlPort:      4190,
		proxyMetricsPort:      4191,
		proxyOutboundCapacity: map[string]uint{},
		tls: "",
	}
}

func (options *proxyConfigOptions) validate() error {
	if !alphaNumDashDot.MatchString(options.linkerdVersion) {
		return fmt.Errorf("%s is not a valid version", options.linkerdVersion)
	}
	if !alphaNumDashDotSlash.MatchString(options.dockerRegistry) {
		return fmt.Errorf("%s is not a valid Docker registry", options.dockerRegistry)
	}
	if options.imagePullPolicy != "Always" && options.imagePullPolicy != "IfNotPresent" && options.imagePullPolicy != "Never" {
		return fmt.Errorf("--image-pull-policy must be one of: Always, IfNotPresent, Never")
	}
	if _, err := time.ParseDuration(options.proxyBindTimeout); err != nil {
		return fmt.Errorf("Invalid duration '%s' for --proxy-bind-timeout flag", options.proxyBindTimeout)
	}
	if options.tls != "" && options.tls != optionalTLS {
		return fmt.Errorf("--tls must be blank or set to \"%s\"", optionalTLS)
	}
	return nil
}

func (options *proxyConfigOptions) enableTLS() bool {
	return options.tls == optionalTLS
}

func (options *proxyConfigOptions) taggedProxyImage() string {
	image := strings.Replace(options.proxyImage, defaultDockerRegistry, options.dockerRegistry, 1)
	return fmt.Sprintf("%s:%s", image, options.linkerdVersion)
}

func (options *proxyConfigOptions) taggedProxyInitImage() string {
	image := strings.Replace(options.initImage, defaultDockerRegistry, options.dockerRegistry, 1)
	return fmt.Sprintf("%s:%s", image, options.linkerdVersion)
}

func addProxyConfigFlags(cmd *cobra.Command, options *proxyConfigOptions) {
	cmd.PersistentFlags().StringVarP(&options.linkerdVersion, "linkerd-version", "v", options.linkerdVersion, "Tag to be used for Linkerd images")
	cmd.PersistentFlags().StringVar(&options.initImage, "init-image", options.initImage, "Linkerd init container image name")
	cmd.PersistentFlags().StringVar(&options.proxyImage, "proxy-image", options.proxyImage, "Linkerd proxy container image name")
	cmd.PersistentFlags().StringVar(&options.dockerRegistry, "registry", options.dockerRegistry, "Docker registry to pull images from")
	cmd.PersistentFlags().StringVar(&options.imagePullPolicy, "image-pull-policy", options.imagePullPolicy, "Docker image pull policy")
	cmd.PersistentFlags().Int64Var(&options.proxyUID, "proxy-uid", options.proxyUID, "Run the proxy under this user ID")
	cmd.PersistentFlags().StringVar(&options.proxyLogLevel, "proxy-log-level", options.proxyLogLevel, "Log level for the proxy")
	cmd.PersistentFlags().StringVar(&options.proxyBindTimeout, "proxy-bind-timeout", options.proxyBindTimeout, "Timeout the proxy will use")
	cmd.PersistentFlags().UintVar(&options.proxyAPIPort, "api-port", options.proxyAPIPort, "Port where the Linkerd controller is running")
	cmd.PersistentFlags().UintVar(&options.proxyControlPort, "control-port", options.proxyControlPort, "Proxy port to use for control")
	cmd.PersistentFlags().UintVar(&options.proxyMetricsPort, "metrics-port", options.proxyMetricsPort, "Proxy port to serve metrics on")
	cmd.PersistentFlags().StringVar(&options.tls, "tls", options.tls, "Enable TLS; valid settings: \"optional\"")
}
