package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/linkerd/linkerd2/pkg/charts"
	cnicharts "github.com/linkerd/linkerd2/pkg/charts/cni"
	"github.com/linkerd/linkerd2/pkg/charts/static"
	"github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/flags"

	// flagspkg "github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/version"
	valuespkg "helm.sh/helm/v3/pkg/cli/values"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli/values"
	"sigs.k8s.io/yaml"
)

var (
	// this doesn't include the namespace-metadata.* templates, which are Helm-only
	templatesCniFiles = []string{
		"templates/cni-plugin.yaml",
	}
)

func newCmdInstallCNIPlugin() *cobra.Command {
	options, err := newCNIInstallOptionsWithDefaults()
	var option values.Options
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	var options valuespkg.Options

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
			return install(os.Stdout, option, options)
		},
	}

	cmd.PersistentFlags().StringVarP(&options.linkerdVersion, "linkerd-version", "v", options.linkerdVersion, "Tag to be used for Linkerd images")
	cmd.PersistentFlags().StringVar(&options.dockerRegistry, "registry", options.dockerRegistry,
		fmt.Sprintf("Docker registry to pull images from ($%s)", flags.EnvOverrideDockerRegistry))
	cmd.PersistentFlags().Int64Var(&options.proxyUID, "proxy-uid", options.proxyUID, "Run the proxy under this user ID")
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
	flags.AddValueOptionsFlags(cmd.Flags(), &option)

	return cmd
}

func install(w io.Writer, values *cnicharts.Values, options valuespkg.Options, registry string) error {

	// Create values override
	valuesOverrides, err := options.MergeValues(nil)
	if err != nil {
		return err
	}

	// TODO: Add any validation logic here

	return renderCNIPlugin(w, values, valuesOverrides, registry)
}


func renderCNIPlugin(w io.Writer, values *cnicharts.Values, valuesOverrides map[string]interface{}, registry string) error {

	files := []*loader.BufferedFile{
		{Name: chartutil.ChartfileName},
		{Name: chartutil.ValuesfileName},
	}

	for _, template := range templatesCniFiles {
		files = append(files, &loader.BufferedFile{Name: template})
	}

	// Load all linkerd-cni chart files into buffer
	if err := charts.FilesReader(static.Templates, cnicharts.HelmDefaultCNIChartDir +"/", files); err != nil {
		return err
	}

func  buildValues(options *cniPluginOptions, valuesOverrides map[string]interface{}) (*cnicharts.Values, error) {
	installValues, err := cnicharts.NewValues()
	if err != nil {
		return nil, err
	}

	if val, exists := valuesOverrides["enablePSP"]; exists {
        if enablePSP, ok := val.(bool); ok {
            installValues.EnablePSP = enablePSP
        } else {
            return nil, fmt.Errorf("invalid type for enablePSP: %T", val)
        }
	}

	if val, exists := valuesOverrides["resources"]; exists {
		jsonVal, err := json.Marshal(val)
		if err != nil {
			return nil, err
		}
		
		var resources cnicharts.Resources
		if err := json.Unmarshal(jsonVal, &resources); err != nil {
			return nil, fmt.Errorf("invalid type for resources: %v", err)
		}
	
		installValues.Resources = resources
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
	installValues.DestCNINetDir = options.destCNINetDir
	installValues.DestCNIBinDir = options.destCNIBinDir
	installValues.UseWaitFlag = options.useWaitFlag
	installValues.PriorityClassName = options.priorityClassName
	return installValues, nil
}

func install(w io.Writer, option values.Options, config *cniPluginOptions) error {

	// Create values override
	valuesOverrides, err := option.MergeValues(nil)
	if err != nil {
		return err
	}

	return renderCNIPlugin(w, valuesOverrides, config)
}

func renderCNIPlugin(w io.Writer, valuesOverrides map[string]interface{}, config *cniPluginOptions) error {

	if err := config.validate(); err != nil {
		return err
	}
	// ....

	values, err := buildValues(config, valuesOverrides)
	if err != nil {
		return err
	}

	// Store final Values generated from values.yaml and CLI flags
	chart.Values = valuesMap

	vals, err := chartutil.CoalesceValues(chart, valuesOverrides)
	if err != nil {
		return err
	}

	regOrig := vals["webhook"].(map[string]interface{})["image"].(map[string]interface{})["name"].(string)
	if registry != "" {
		vals["webhook"].(map[string]interface{})["image"].(map[string]interface{})["name"] = cmd.RegistryOverride(regOrig, registry)
	}
	// env var overrides CLI flag
	if override := os.Getenv(flags.EnvOverrideDockerRegistry); override != "" {
		vals["webhook"].(map[string]interface{})["image"].(map[string]interface{})["name"] = cmd.RegistryOverride(regOrig, override)
	}

	fullValues := map[string]interface{}{
		"Values": vals,
		"Release": map[string]interface{}{
			"Namespace": defaultCNINamespace,
			"Service":   "CLI",
		},
	}


	// Attach the final values into the `Values` field for rendering to work
	renderedTemplates, err := engine.Render(chart, fullValues)
	if err != nil {
		return err
	}

	// Merge templates and inject
	var buf bytes.Buffer
	for _, tmpl := range chart.Templates {
		t := path.Join(chart.Metadata.Name, tmpl.Name)
		if _, err := buf.WriteString(renderedTemplates[t]); err != nil {
			return err
		}
	}

	_, err = w.Write(buf.Bytes())
	return err
}
