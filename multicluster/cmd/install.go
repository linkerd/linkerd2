package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/linkerd/linkerd2/multicluster/static"
	multicluster "github.com/linkerd/linkerd2/multicluster/values"
	"github.com/linkerd/linkerd2/pkg/charts"
	partials "github.com/linkerd/linkerd2/pkg/charts/static"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	api "github.com/linkerd/linkerd2/pkg/public"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chart/loader"
	chartloader "helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	valuespkg "helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/engine"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/yaml"
)

type (
	multiclusterInstallOptions struct {
		gateway                 multicluster.Gateway
		remoteMirrorCredentials bool
	}
)

func newMulticlusterInstallCommand() *cobra.Command {
	options, err := newMulticlusterInstallOptionsWithDefault()
	var ha bool
	var wait time.Duration
	var valuesOptions valuespkg.Options
	var ignoreCluster bool

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Output Kubernetes configs to install the Linkerd multicluster add-on",
		Args:  cobra.NoArgs,
		Example: `  # Default install.
  linkerd multicluster install | kubectl apply -f -

The installation can be configured by using the --set, --values, --set-string and --set-file flags.
A full list of configurable values can be found at https://github.com/linkerd/linkerd2/blob/main/multicluster/charts/linkerd-multicluster/README.md
  `,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !ignoreCluster {
				// Wait for the core control-plane to be up and running
				api.CheckPublicAPIClientOrRetryOrExit(healthcheck.Options{
					ControlPlaneNamespace: controlPlaneNamespace,
					KubeConfig:            kubeconfigPath,
					KubeContext:           kubeContext,
					Impersonate:           impersonate,
					ImpersonateGroup:      impersonateGroup,
					APIAddr:               apiAddr,
					RetryDeadline:         time.Now().Add(wait),
				})
			}
			return install(cmd.Context(), stdout, options, valuesOptions, ha, ignoreCluster)
		},
	}

	flags.AddValueOptionsFlags(cmd.Flags(), &valuesOptions)
	cmd.Flags().BoolVar(&options.gateway.Enabled, "gateway", options.gateway.Enabled, "If the gateway component should be installed")
	cmd.Flags().Uint32Var(&options.gateway.Port, "gateway-port", options.gateway.Port, "The port on the gateway used for all incoming traffic")
	cmd.Flags().Uint32Var(&options.gateway.Probe.Seconds, "gateway-probe-seconds", options.gateway.Probe.Seconds, "The interval at which the gateway will be checked for being alive in seconds")
	cmd.Flags().Uint32Var(&options.gateway.Probe.Port, "gateway-probe-port", options.gateway.Probe.Port, "The liveness check port of the gateway")
	cmd.Flags().BoolVar(&options.remoteMirrorCredentials, "service-mirror-credentials", options.remoteMirrorCredentials, "Whether to install the service account which can be used by service mirror components in source clusters to discover exported services")
	cmd.Flags().StringVar(&options.gateway.ServiceType, "gateway-service-type", options.gateway.ServiceType, "Overwrite Service type for gateway service")
	cmd.Flags().BoolVar(&ha, "ha", false, `Install multicluster extension in High Availability mode.`)
	cmd.Flags().DurationVar(&wait, "wait", 300*time.Second, "Wait for core control-plane components to be available")
	cmd.Flags().BoolVar(&ignoreCluster, "ignore-cluster", false,
		"Ignore the current Kubernetes cluster when checking for existing cluster configuration (default false)")

	// Hide developer focused flags in release builds.
	release, err := version.IsReleaseChannel(version.Version)
	if err != nil {
		log.Errorf("Unable to parse version: %s", version.Version)
	}
	if release {
		cmd.Flags().MarkHidden("control-plane-version")
		cmd.Flags().MarkHidden("gateway-nginx-image")
		cmd.Flags().MarkHidden("gateway-nginx-image-version")
	}

	return cmd
}

func install(ctx context.Context, w io.Writer, options *multiclusterInstallOptions, valuesOptions valuespkg.Options, ha, ignoreCluster bool) error {
	values, err := buildMulticlusterInstallValues(ctx, options, ignoreCluster)
	if err != nil {
		return err
	}

	// Create values override
	valuesOverrides, err := valuesOptions.MergeValues(nil)
	if err != nil {
		return err
	}

	if ha {
		valuesOverrides, err = charts.OverrideFromFile(valuesOverrides, static.Templates, helmMulticlusterDefaultChartName, "values-ha.yaml")
		if err != nil {
			return err
		}
	}

	return render(w, values, valuesOverrides)
}

func render(w io.Writer, values *multicluster.Values, valuesOverrides map[string]interface{}) error {
	files := []*chartloader.BufferedFile{
		{Name: chartutil.ChartfileName},
		{Name: chartutil.ValuesfileName},
		{Name: "templates/namespace.yaml"},
		{Name: "templates/gateway.yaml"},
		{Name: "templates/proxy-admin-policy.yaml"},
		{Name: "templates/gateway-policy.yaml"},
		{Name: "templates/psp.yaml"},
		{Name: "templates/remote-access-service-mirror-rbac.yaml"},
		{Name: "templates/link-crd.yaml"},
		{Name: "templates/service-mirror-policy.yaml"},
	}

	var partialFiles []*loader.BufferedFile
	for _, template := range charts.L5dPartials {
		partialFiles = append(partialFiles,
			&loader.BufferedFile{Name: template},
		)
	}

	// Load all multicluster install chart files into buffer
	if err := charts.FilesReader(static.Templates, helmMulticlusterDefaultChartName+"/", files); err != nil {
		return err
	}

	// Load all partial chart files into buffer
	if err := charts.FilesReader(partials.Templates, "", partialFiles); err != nil {
		return err
	}

	// Create a Chart obj from the files
	chart, err := loader.LoadFiles(append(files, partialFiles...))
	if err != nil {
		return err
	}

	// Render raw values and create chart config
	rawValues, err := yaml.Marshal(values)
	if err != nil {
		return err
	}
	// Store final Values generated from values.yaml and CLI flags
	err = yaml.Unmarshal(rawValues, &chart.Values)
	if err != nil {
		return err
	}

	vals, err := chartutil.CoalesceValues(chart, valuesOverrides)
	if err != nil {
		return err
	}

	fullValues := map[string]interface{}{
		"Values": vals,
		"Release": map[string]interface{}{
			"Namespace": defaultMulticlusterNamespace,
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
	w.Write(buf.Bytes())
	w.Write([]byte("---\n"))

	return nil
}

func newMulticlusterInstallOptionsWithDefault() (*multiclusterInstallOptions, error) {
	defaults, err := multicluster.NewInstallValues()
	if err != nil {
		return nil, err
	}

	return &multiclusterInstallOptions{
		gateway:                 *defaults.Gateway,
		remoteMirrorCredentials: true,
	}, nil
}

func buildMulticlusterInstallValues(ctx context.Context, opts *multiclusterInstallOptions, ignoreCluster bool) (*multicluster.Values, error) {
	defaults, err := multicluster.NewInstallValues()
	if err != nil {
		return nil, err
	}

	defaults.Gateway.Enabled = opts.gateway.Enabled
	defaults.Gateway.Port = opts.gateway.Port
	defaults.Gateway.Probe.Seconds = opts.gateway.Probe.Seconds
	defaults.Gateway.Probe.Port = opts.gateway.Probe.Port
	defaults.LinkerdNamespace = controlPlaneNamespace
	defaults.LinkerdVersion = version.Version
	defaults.RemoteMirrorServiceAccount = opts.remoteMirrorCredentials
	defaults.Gateway.ServiceType = opts.gateway.ServiceType

	if ignoreCluster {
		return defaults, nil
	}

	values, err := getLinkerdConfigMap(ctx)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil, errors.New("you need Linkerd to be installed in order to install multicluster addons")
		}
		return nil, err
	}
	defaults.ProxyOutboundPort = uint32(values.Proxy.Ports.Outbound)
	defaults.IdentityTrustDomain = values.IdentityTrustDomain

	return defaults, nil
}
