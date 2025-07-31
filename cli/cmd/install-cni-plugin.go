package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/linkerd/linkerd2/charts"
	chartspkg "github.com/linkerd/linkerd2/pkg/charts"
	cnicharts "github.com/linkerd/linkerd2/pkg/charts/cni"
	"github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli/values"
)

const (
	helmCNIDefaultChartName = "linkerd2-cni"
	helmCNIDefaultChartDir  = "linkerd2-cni"
)

type cniPluginImage struct {
	name       string
	version    string
	pullPolicy interface{}
}

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
	proxyGID            int64
	image               cniPluginImage
	logLevel            string
	destCNINetDir       string
	destCNIBinDir       string
	useWaitFlag         bool
	priorityClassName   string
}

func (options *cniPluginOptions) validate() error {
	if !alphaNumDashDot.MatchString(options.linkerdVersion) {
		return fmt.Errorf("%s is not a valid version", options.linkerdVersion)
	}

	if !alphaNumDashDotSlashColon.MatchString(options.dockerRegistry) {
		return fmt.Errorf("%s is not a valid Docker registry. The url can contain only letters, numbers, dash, dot, slash and colon", options.dockerRegistry)
	}

	if _, err := log.ParseLevel(options.logLevel); err != nil {
		return fmt.Errorf("--cni-log-level must be one of: panic, fatal, error, warn, info, debug, trace")
	}

	if err := validateRangeSlice(options.ignoreInboundPorts); err != nil {
		return err
	}

	if err := validateRangeSlice(options.ignoreOutboundPorts); err != nil {
		return err
	}
	return nil
}

func (options *cniPluginOptions) pluginImage() cnicharts.Image {
	image := cnicharts.Image{
		Name:       options.image.name,
		Version:    options.image.version,
		PullPolicy: options.image.pullPolicy,
	}
	// env var overrides CLI flag
	if override := os.Getenv(flags.EnvOverrideDockerRegistry); override != "" {
		image.Name = cmd.RegistryOverride(options.image.name, override)
		return image
	}
	if options.dockerRegistry != cmd.DefaultDockerRegistry {
		image.Name = cmd.RegistryOverride(options.image.name, options.dockerRegistry)
		return image
	}
	return image
}

func newCmdInstallCNIPlugin() *cobra.Command {
	options, err := newCNIInstallOptionsWithDefaults()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var valOpts values.Options

	cmd := &cobra.Command{
		Use:   "install-cni [flags]",
		Short: "Output Kubernetes configs to install Linkerd CNI",
		Long: `Output Kubernetes configs to install Linkerd CNI.

This command installs a DaemonSet into the Linkerd control plane. The DaemonSet
copies the necessary linkerd-cni plugin binaries and configs onto the host. It
assumes that the 'linkerd install' command will be executed with the
'--linkerd-cni-enabled' flag. This command needs to be executed before the
'linkerd install --linkerd-cni-enabled' command.

The installation can be configured by using the --set, --values, --set-string and --set-file flags. A full list of configurable values can be found at https://artifacthub.io/packages/helm/linkerd2/linkerd2-cni#values`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return renderCNIPlugin(os.Stdout, valOpts, options)
		},
	}

	cmd.PersistentFlags().StringVarP(&options.linkerdVersion, "linkerd-version", "v", options.linkerdVersion, "Tag to be used for Linkerd images")
	cmd.PersistentFlags().StringVar(&options.dockerRegistry, "registry", options.dockerRegistry,
		fmt.Sprintf("Docker registry to pull images from ($%s)", flags.EnvOverrideDockerRegistry))
	cmd.PersistentFlags().Int64Var(&options.proxyUID, "proxy-uid", options.proxyUID, "Run the proxy under this user ID")
	cmd.PersistentFlags().Int64Var(&options.proxyGID, "proxy-gid", options.proxyGID, "Run the proxy under this group ID")
	cmd.PersistentFlags().UintVar(&options.inboundPort, "inbound-port", options.inboundPort, "Proxy port to use for inbound traffic")
	cmd.PersistentFlags().UintVar(&options.outboundPort, "outbound-port", options.outboundPort, "Proxy port to use for outbound traffic")
	cmd.PersistentFlags().UintVar(&options.proxyControlPort, "control-port", options.proxyControlPort, "Proxy port to use for control")
	cmd.PersistentFlags().UintVar(&options.proxyAdminPort, "admin-port", options.proxyAdminPort, "Proxy port to serve metrics on")
	cmd.PersistentFlags().StringSliceVar(&options.ignoreInboundPorts, "skip-inbound-ports", options.ignoreInboundPorts, "Ports and/or port ranges (inclusive) that should skip the proxy and send directly to the application")
	cmd.PersistentFlags().StringSliceVar(&options.ignoreOutboundPorts, "skip-outbound-ports", options.ignoreOutboundPorts, "Outbound ports and/or port ranges (inclusive) that should skip the proxy")
	cmd.PersistentFlags().UintSliceVar(&options.portsToRedirect, "redirect-ports", options.portsToRedirect, "Ports to redirect to proxy, if no port is specified then ALL ports are redirected")
	cmd.PersistentFlags().StringVar(&options.image.name, "cni-image", options.image.name, "Image for the cni-plugin")
	cmd.PersistentFlags().StringVar(&options.image.version, "cni-image-version", options.image.version, "Image Version for the cni-plugin")
	cmd.PersistentFlags().StringVar(&options.logLevel, "cni-log-level", options.logLevel, "Log level for the cni-plugin")
	cmd.PersistentFlags().StringVar(&options.destCNINetDir, "dest-cni-net-dir", options.destCNINetDir, "Directory on the host where the CNI configuration will be placed")
	cmd.PersistentFlags().StringVar(&options.destCNIBinDir, "dest-cni-bin-dir", options.destCNIBinDir, "Directory on the host where the CNI binary will be placed")
	cmd.PersistentFlags().StringVar(&options.priorityClassName, "priority-class-name", options.priorityClassName, "Pod priorityClassName for CNI daemonset's pods")
	cmd.PersistentFlags().BoolVar(
		&options.useWaitFlag,
		"use-wait-flag",
		options.useWaitFlag,
		"Configures the CNI plugin to use the \"-w\" flag for the iptables command. (default false)")

	flags.AddValueOptionsFlags(cmd.Flags(), &valOpts)

	return cmd
}

func newCNIInstallOptionsWithDefaults() (*cniPluginOptions, error) {
	defaults, err := cnicharts.NewValues()
	if err != nil {
		return nil, err
	}

	cniPluginImage := cniPluginImage{
		name:    cmd.DefaultDockerRegistry + "/cni-plugin",
		version: version.LinkerdCNIVersion,
	}

	cniOptions := cniPluginOptions{
		linkerdVersion:      version.Version,
		dockerRegistry:      cmd.DefaultDockerRegistry,
		proxyControlPort:    4190,
		proxyAdminPort:      4191,
		inboundPort:         defaults.InboundProxyPort,
		outboundPort:        defaults.OutboundProxyPort,
		ignoreInboundPorts:  nil,
		ignoreOutboundPorts: nil,
		proxyUID:            defaults.ProxyUID,
		proxyGID:            defaults.ProxyGID,
		image:               cniPluginImage,
		logLevel:            "info",
		destCNINetDir:       defaults.DestCNINetDir,
		destCNIBinDir:       defaults.DestCNIBinDir,
		useWaitFlag:         defaults.UseWaitFlag,
		priorityClassName:   defaults.PriorityClassName,
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

	portsToRedirect := []string{}
	for _, p := range options.portsToRedirect {
		portsToRedirect = append(portsToRedirect, fmt.Sprintf("%d", p))
	}

	installValues.Image = options.pluginImage()
	installValues.LogLevel = options.logLevel
	installValues.InboundProxyPort = options.inboundPort
	installValues.OutboundProxyPort = options.outboundPort
	installValues.IgnoreInboundPorts = strings.Join(options.ignoreInboundPorts, ",")
	installValues.IgnoreOutboundPorts = strings.Join(options.ignoreOutboundPorts, ",")
	installValues.PortsToRedirect = strings.Join(portsToRedirect, ",")
	installValues.ProxyUID = options.proxyUID
	installValues.ProxyGID = options.proxyGID
	installValues.DestCNINetDir = options.destCNINetDir
	installValues.DestCNIBinDir = options.destCNIBinDir
	installValues.UseWaitFlag = options.useWaitFlag
	installValues.PriorityClassName = options.priorityClassName
	return installValues, nil
}

func renderCNIPlugin(w io.Writer, valOpts values.Options, config *cniPluginOptions) error {

	if err := config.validate(); err != nil {
		return err
	}

	valuesOverrides, err := valOpts.MergeValues(nil)
	if err != nil {
		return err
	}

	values, err := config.buildValues()
	if err != nil {
		return err
	}

	mapValues, err := values.ToMap()
	if err != nil {
		return err
	}

	valuesWrapper := &chart.Chart{
		Metadata: &chart.Metadata{Name: ""},
		Values:   mapValues,
	}
	mergedValues, err := chartutil.CoalesceValues(valuesWrapper, valuesOverrides)
	if err != nil {
		return err
	}

	files := []*loader.BufferedFile{
		{Name: chartutil.ChartfileName},
		{Name: "templates/cni-plugin.yaml"},
	}

	ch := &chartspkg.Chart{
		Name:      helmCNIDefaultChartName,
		Dir:       helmCNIDefaultChartDir,
		Namespace: defaultCNINamespace,
		Values:    mergedValues,
		Files:     files,
		Fs:        charts.Templates,
	}

	buf, err := ch.RenderCNI()
	if err != nil {
		return err
	}
	w.Write(buf.Bytes())
	w.Write([]byte("---\n"))

	return nil
}
