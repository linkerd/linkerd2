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
		namespace               string
		remoteMirrorCredentials bool
	}
)

func newMulticlusterInstallCommand() *cobra.Command {
	options, err := newMulticlusterInstallOptionsWithDefault()
	var wait time.Duration
	var valuesOptions valuespkg.Options

	if err != nil {
		fmt.Fprintf(os.Stderr, "%s", err)
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
			return install(cmd.Context(), stdout, options, valuesOptions)
		},
	}

	flags.AddValueOptionsFlags(cmd.Flags(), &valuesOptions)
	cmd.Flags().StringVar(&options.namespace, "namespace", options.namespace, "The namespace in which the multicluster add-on is to be installed. Must not be the control plane namespace. ")
	cmd.Flags().BoolVar(&options.gateway.Enabled, "gateway", options.gateway.Enabled, "If the gateway component should be installed")
	cmd.Flags().Uint32Var(&options.gateway.Port, "gateway-port", options.gateway.Port, "The port on the gateway used for all incoming traffic")
	cmd.Flags().Uint32Var(&options.gateway.Probe.Seconds, "gateway-probe-seconds", options.gateway.Probe.Seconds, "The interval at which the gateway will be checked for being alive in seconds")
	cmd.Flags().Uint32Var(&options.gateway.Probe.Port, "gateway-probe-port", options.gateway.Probe.Port, "The liveness check port of the gateway")
	cmd.Flags().BoolVar(&options.remoteMirrorCredentials, "service-mirror-credentials", options.remoteMirrorCredentials, "Whether to install the service account which can be used by service mirror components in source clusters to discover exported services")
	cmd.Flags().StringVar(&options.gateway.ServiceType, "gateway-service-type", options.gateway.ServiceType, "Overwrite Service type for gateway service")
	cmd.Flags().DurationVar(&wait, "wait", 300*time.Second, "Wait for core control-plane components to be available")

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

func install(ctx context.Context, w io.Writer, options *multiclusterInstallOptions, valuesOptions valuespkg.Options) error {
	values, err := buildMulticlusterInstallValues(ctx, options)
	if err != nil {
		return err
	}

	// Create values override
	valuesOverrides, err := valuesOptions.MergeValues(nil)
	if err != nil {
		return err
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

	// Attach the final values into the `Values` field for rendering to work
	renderedTemplates, err := engine.Render(chart, map[string]interface{}{"Values": vals})
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
		namespace:               defaults.Namespace,
		remoteMirrorCredentials: true,
	}, nil
}

func buildMulticlusterInstallValues(ctx context.Context, opts *multiclusterInstallOptions) (*multicluster.Values, error) {

	values, err := getLinkerdConfigMap(ctx)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil, errors.New("you need Linkerd to be installed in order to install multicluster addons")
		}
		return nil, err
	}

	if opts.namespace == "" {
		return nil, errors.New("you need to specify a namespace")
	}

	if opts.namespace == controlPlaneNamespace {
		return nil, errors.New("you need to setup the multicluster addons in a namespace different than the Linkerd one")
	}

	defaults, err := multicluster.NewInstallValues()
	if err != nil {
		return nil, err
	}

	defaults.Namespace = opts.namespace
	defaults.Gateway.Enabled = opts.gateway.Enabled
	defaults.Gateway.Port = opts.gateway.Port
	defaults.Gateway.Probe.Seconds = opts.gateway.Probe.Seconds
	defaults.Gateway.Probe.Port = opts.gateway.Probe.Port
	defaults.IdentityTrustDomain = values.IdentityTrustDomain
	defaults.LinkerdNamespace = controlPlaneNamespace
	defaults.ProxyOutboundPort = uint32(values.Proxy.Ports.Outbound)
	defaults.LinkerdVersion = version.Version
	defaults.RemoteMirrorServiceAccount = opts.remoteMirrorCredentials
	defaults.Gateway.ServiceType = opts.gateway.ServiceType

	return defaults, nil
}
