package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/runconduit/conduit/controller/k8s"
	"github.com/runconduit/conduit/pkg/version"
	"github.com/spf13/cobra"
)

var (
	initImage           string
	proxyImage          string
	proxyUID            int64
	inboundPort         uint
	outboundPort        uint
	ignoreInboundPorts  []uint
	ignoreOutboundPorts []uint
	proxyControlPort    uint
	proxyAPIPort        uint
	proxyLogLevel       string
)

var injectCmd = &cobra.Command{
	Use:   "inject [flags] CONFIG-FILE",
	Short: "Add the Conduit proxy to a Kubernetes config",
	Long: `Add the Conduit proxy to a Kubernetes config.

You can use a config file from stdin by using the '-' argument
with 'conduit inject'. e.g. curl http://url.to/yml | conduit inject -
	`,
	RunE: func(cmd *cobra.Command, args []string) error {

		if len(args) < 1 {
			return fmt.Errorf("please specify a deployment file")
		}

		var in io.Reader
		var err error

		if args[0] == "-" {
			in = os.Stdin
		} else {
			if in, err = os.Open(args[0]); err != nil {
				return err
			}
		}

		podConfig := k8s.PodConfig{
			initImage,
			proxyImage,
			proxyUID,
			inboundPort,
			outboundPort,
			ignoreInboundPorts,
			ignoreOutboundPorts,
			proxyControlPort,
			proxyAPIPort,
			proxyLogLevel,
			conduitVersion,
			imagePullPolicy,
			controlPlaneNamespace,
		}
		return k8s.InjectYAML(in, os.Stdout, podConfig)
	},
}

func init() {
	RootCmd.AddCommand(injectCmd)
	injectCmd.PersistentFlags().StringVarP(&conduitVersion, "conduit-version", "v", version.Version, "tag to be used for Conduit images")
	injectCmd.PersistentFlags().StringVar(&initImage, "init-image", "gcr.io/runconduit/proxy-init", "Conduit init container image name")
	injectCmd.PersistentFlags().StringVar(&proxyImage, "proxy-image", "gcr.io/runconduit/proxy", "Conduit proxy container image name")
	injectCmd.PersistentFlags().StringVar(&imagePullPolicy, "image-pull-policy", "IfNotPresent", "Docker image pull policy")
	injectCmd.PersistentFlags().Int64Var(&proxyUID, "proxy-uid", 2102, "Run the proxy under this user ID")
	injectCmd.PersistentFlags().UintVar(&inboundPort, "inbound-port", 4143, "proxy port to use for inbound traffic")
	injectCmd.PersistentFlags().UintVar(&outboundPort, "outbound-port", 4140, "proxy port to use for outbound traffic")
	injectCmd.PersistentFlags().UintSliceVar(&ignoreInboundPorts, "skip-inbound-ports", nil, "ports that should skip the proxy and send directly to the application")
	injectCmd.PersistentFlags().UintSliceVar(&ignoreOutboundPorts, "skip-outbound-ports", nil, "outbound ports that should skip the proxy")
	injectCmd.PersistentFlags().UintVar(&proxyControlPort, "control-port", 4190, "proxy port to use for control")
	injectCmd.PersistentFlags().UintVar(&proxyAPIPort, "api-port", 8086, "port where the Conduit controller is running")
	injectCmd.PersistentFlags().StringVar(&proxyLogLevel, "proxy-log-level", "warn,conduit_proxy=info", "log level for the proxy")
}
