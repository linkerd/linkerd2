package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/linkerd/linkerd2/pkg/charts"
	cnicharts "github.com/linkerd/linkerd2/pkg/charts/cni"
	"github.com/linkerd/linkerd2/pkg/charts/static"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

const (
	helmCNIDefaultChartName = "linkerd2-cni"
	helmCNIDefaultChartDir  = "linkerd2-cni"
)

type cniPluginOptions struct {
	linkerdVersion      string
	dockerRegistry      string
	proxyControlPort    uint
	proxyAdminPort      uint
	inboundPort         uint
	outboundPort        uint
	ignoreInboundPorts  []string
	ignoreOutboundPorts []string
	portsToRedirect     []uint
	proxyUID            int64
	cniPluginImage      string
	logLevel            string
	destCNINetDir       string
	destCNIBinDir       string
	useWaitFlag         bool
	priorityClassName   string
	installNamespace    bool
}

func (options *cniPluginOptions) validate() error {
	if !alphaNumDashDot.MatchString(options.linkerdVersion) {
		return fmt.Errorf("%s is not a valid version", options.linkerdVersion)
	}

	if !alphaNumDashDotSlashColon.MatchString(options.dockerRegistry) {
		return fmt.Errorf("%s is not a valid Docker registry. The url can contain only letters, numbers, dash, dot, slash and colon", options.dockerRegistry)
	}

	if _, err := log.ParseLevel(options.logLevel); err != nil {
		return fmt.Errorf("--cni-log-level must be one of: panic, fatal, error, warn, info, debug")
	}

	if err := validateRangeSlice(options.ignoreInboundPorts); err != nil {
		return err
	}

	if err := validateRangeSlice(options.ignoreOutboundPorts); err != nil {
		return err
	}
	return nil
}

func (options *cniPluginOptions) pluginImage() string {
	if options.dockerRegistry != defaultDockerRegistry {
		return registryOverride(options.cniPluginImage, options.dockerRegistry)
	}
	return options.cniPluginImage
}

func newCmdInstallCNIPlugin() *cobra.Command {
	options, err := newCNIInstallOptionsWithDefaults()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s", err)
		os.Exit(1)
	}

	cmd := &cobra.Command{
		Use:   "install-cni [flags]",
		Short: "Output Kubernetes configs to install Linkerd CNI",
		Long: `Output Kubernetes configs to install Linkerd CNI.

This command installs a DaemonSet into the Linkerd control plane. The DaemonSet
copies the necessary linkerd-cni plugin binaries and configs onto the host. It
assumes that the 'linkerd install' command will be executed with the
'--linkerd-cni-enabled' flag. This command needs to be executed before the
'linkerd install --linkerd-cni-enabled' command.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return renderCNIPlugin(os.Stdout, options)
		},
	}

	cmd.PersistentFlags().StringVarP(&options.linkerdVersion, "linkerd-version", "v", options.linkerdVersion, "Tag to be used for Linkerd images")
	cmd.PersistentFlags().StringVar(&options.dockerRegistry, "registry", options.dockerRegistry, "Docker registry to pull images from")
	cmd.PersistentFlags().Int64Var(&options.proxyUID, "proxy-uid", options.proxyUID, "Run the proxy under this user ID")
	cmd.PersistentFlags().UintVar(&options.inboundPort, "inbound-port", options.inboundPort, "Proxy port to use for inbound traffic")
	cmd.PersistentFlags().UintVar(&options.outboundPort, "outbound-port", options.outboundPort, "Proxy port to use for outbound traffic")
	cmd.PersistentFlags().UintVar(&options.proxyControlPort, "control-port", options.proxyControlPort, "Proxy port to use for control")
	cmd.PersistentFlags().UintVar(&options.proxyAdminPort, "admin-port", options.proxyAdminPort, "Proxy port to serve metrics on")
	cmd.PersistentFlags().StringSliceVar(&options.ignoreInboundPorts, "skip-inbound-ports", options.ignoreInboundPorts, "Ports and/or port ranges (inclusive) that should skip the proxy and send directly to the application")
	cmd.PersistentFlags().StringSliceVar(&options.ignoreOutboundPorts, "skip-outbound-ports", options.ignoreOutboundPorts, "Outbound ports and/or port ranges (inclusive) that should skip the proxy")
	cmd.PersistentFlags().UintSliceVar(&options.portsToRedirect, "redirect-ports", options.portsToRedirect, "Ports to redirect to proxy, if no port is specified then ALL ports are redirected")
	cmd.PersistentFlags().StringVar(&options.cniPluginImage, "cni-image", options.cniPluginImage, "Image for the cni-plugin")
	cmd.PersistentFlags().StringVar(&options.logLevel, "cni-log-level", options.logLevel, "Log level for the cni-plugin")
	cmd.PersistentFlags().StringVar(&options.destCNINetDir, "dest-cni-net-dir", options.destCNINetDir, "Directory on the host where the CNI configuration will be placed")
	cmd.PersistentFlags().StringVar(&options.destCNIBinDir, "dest-cni-bin-dir", options.destCNIBinDir, "Directory on the host where the CNI binary will be placed")
	cmd.PersistentFlags().StringVar(&options.priorityClassName, "priority-class-name", options.priorityClassName, "Pod priorityClassName for CNI daemonset's pods")
	cmd.PersistentFlags().BoolVar(&options.installNamespace, "install-namespace", options.installNamespace, "Whether to create the CNI namespace or not")
	cmd.PersistentFlags().BoolVar(
		&options.useWaitFlag,
		"use-wait-flag",
		options.useWaitFlag,
		"Configures the CNI plugin to use the \"-w\" flag for the iptables command. (default false)")

	return cmd
}

func newCNIInstallOptionsWithDefaults() (*cniPluginOptions, error) {
	defaults, err := cnicharts.NewValues()
	if err != nil {
		return nil, err
	}
	cniOptions := cniPluginOptions{
		linkerdVersion:      version.Version,
		dockerRegistry:      defaultDockerRegistry,
		proxyControlPort:    4190,
		proxyAdminPort:      4191,
		inboundPort:         defaults.InboundProxyPort,
		outboundPort:        defaults.OutboundProxyPort,
		ignoreInboundPorts:  nil,
		ignoreOutboundPorts: nil,
		proxyUID:            defaults.ProxyUID,
		cniPluginImage:      defaultDockerRegistry + "/cni-plugin",
		logLevel:            "info",
		destCNINetDir:       defaults.DestCNINetDir,
		destCNIBinDir:       defaults.DestCNIBinDir,
		useWaitFlag:         defaults.UseWaitFlag,
		priorityClassName:   defaults.PriorityClassName,
		installNamespace:    defaults.InstallNamespace,
	}

	if defaults.IgnoreInboundPorts != "" {
		cniOptions.ignoreInboundPorts = strings.Split(defaults.IgnoreInboundPorts, ",")

	}
	if defaults.IgnoreOutboundPorts != "" {
		cniOptions.ignoreOutboundPorts = strings.Split(defaults.IgnoreOutboundPorts, ",")
	}

	return &cniOptions, nil
}

func (options *cniPluginOptions) buildValues() (*cnicharts.Values, error) {
	installValues, err := cnicharts.NewValues()
	if err != nil {
		return nil, err
	}

	ignoreInboundPorts := []string{
		fmt.Sprintf("%d", options.proxyControlPort),
		fmt.Sprintf("%d", options.proxyAdminPort),
	}

	ignoreInboundPorts = append(ignoreInboundPorts, options.ignoreInboundPorts...)

	portsToRedirect := []string{}
	for _, p := range options.portsToRedirect {
		portsToRedirect = append(portsToRedirect, fmt.Sprintf("%d", p))
	}

	installValues.CNIPluginImage = options.pluginImage()
	installValues.CNIPluginVersion = options.linkerdVersion
	installValues.LogLevel = options.logLevel
	installValues.InboundProxyPort = options.inboundPort
	installValues.OutboundProxyPort = options.outboundPort
	installValues.IgnoreInboundPorts = strings.Join(ignoreInboundPorts, ",")
	installValues.IgnoreOutboundPorts = strings.Join(options.ignoreOutboundPorts, ",")
	installValues.PortsToRedirect = strings.Join(portsToRedirect, ",")
	installValues.ProxyUID = options.proxyUID
	installValues.DestCNINetDir = options.destCNINetDir
	installValues.DestCNIBinDir = options.destCNIBinDir
	installValues.UseWaitFlag = options.useWaitFlag
	installValues.Namespace = cniNamespace
	installValues.PriorityClassName = options.priorityClassName
	installValues.InstallNamespace = options.installNamespace
	return installValues, nil
}

func renderCNIPlugin(w io.Writer, config *cniPluginOptions) error {

	if err := config.validate(); err != nil {
		return err
	}

	values, err := config.buildValues()
	if err != nil {
		return err
	}

	// Render raw values and create chart config
	rawValues, err := yaml.Marshal(values)
	if err != nil {
		return err
	}

	files := []*loader.BufferedFile{
		{Name: chartutil.ChartfileName},
		{Name: "templates/cni-plugin.yaml"},
	}

	chart := &charts.Chart{
		Name:      helmCNIDefaultChartName,
		Dir:       helmCNIDefaultChartDir,
		Namespace: controlPlaneNamespace,
		RawValues: rawValues,
		Files:     files,
		Fs:        static.Templates,
	}
	buf, err := chart.RenderCNI()
	if err != nil {
		return err
	}
	w.Write(buf.Bytes())
	w.Write([]byte("---\n"))

	return nil
}
