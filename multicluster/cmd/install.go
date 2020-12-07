package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/linkerd/linkerd2/multicluster/static"
	multicluster "github.com/linkerd/linkerd2/multicluster/values"
	"github.com/linkerd/linkerd2/pkg/charts"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	chartloader "helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/yaml"
)

type (
	multiclusterInstallOptions struct {
		gateway                 bool
		gatewayPort             uint32
		gatewayProbeSeconds     uint32
		gatewayProbePort        uint32
		namespace               string
		gatewayNginxImage       string
		gatewayNginxVersion     string
		remoteMirrorCredentials bool
	}
)

func newMulticlusterInstallCommand() *cobra.Command {
	options, err := newMulticlusterInstallOptionsWithDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s", err)
		os.Exit(1)
	}

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Output Kubernetes configs to install the Linkerd multicluster add-on",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {

			values, err := buildMulticlusterInstallValues(cmd.Context(), options)

			if err != nil {
				return err
			}

			// Render raw values and create chart config
			rawValues, err := yaml.Marshal(values)
			if err != nil {
				return err
			}

			files := []*chartloader.BufferedFile{
				{Name: chartutil.ChartfileName},
				{Name: "templates/namespace.yaml"},
				{Name: "templates/gateway.yaml"},
				{Name: "templates/remote-access-service-mirror-rbac.yaml"},
				{Name: "templates/link-crd.yaml"},
			}

			chart := &charts.Chart{
				Name:      helmMulticlusterDefaultChartName,
				Dir:       helmMulticlusterDefaultChartName,
				Namespace: controlPlaneNamespace,
				RawValues: rawValues,
				Files:     files,
				Fs:        static.Templates,
			}
			buf, err := chart.RenderNoPartials()
			if err != nil {
				return err
			}
			stdout.Write(buf.Bytes())
			stdout.Write([]byte("---\n"))

			return nil
		},
	}

	cmd.Flags().StringVar(&options.namespace, "namespace", options.namespace, "The namespace in which the multicluster add-on is to be installed. Must not be the control plane namespace. ")
	cmd.Flags().BoolVar(&options.gateway, "gateway", options.gateway, "If the gateway component should be installed")
	cmd.Flags().Uint32Var(&options.gatewayPort, "gateway-port", options.gatewayPort, "The port on the gateway used for all incoming traffic")
	cmd.Flags().Uint32Var(&options.gatewayProbeSeconds, "gateway-probe-seconds", options.gatewayProbeSeconds, "The interval at which the gateway will be checked for being alive in seconds")
	cmd.Flags().Uint32Var(&options.gatewayProbePort, "gateway-probe-port", options.gatewayProbePort, "The liveness check port of the gateway")
	cmd.Flags().StringVar(&options.gatewayNginxImage, "gateway-nginx-image", options.gatewayNginxImage, "The nginx image to be used")
	cmd.Flags().StringVar(&options.gatewayNginxVersion, "gateway-nginx-image-version", options.gatewayNginxVersion, "The version of nginx to be used")
	cmd.Flags().BoolVar(&options.remoteMirrorCredentials, "service-mirror-credentials", options.remoteMirrorCredentials, "Whether to install the service account which can be used by service mirror components in source clusters to discover exported services")

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

func newMulticlusterInstallOptionsWithDefault() (*multiclusterInstallOptions, error) {
	defaults, err := multicluster.NewInstallValues()
	if err != nil {
		return nil, err
	}

	return &multiclusterInstallOptions{
		gateway:                 defaults.Gateway,
		gatewayPort:             defaults.GatewayPort,
		gatewayProbeSeconds:     defaults.GatewayProbeSeconds,
		gatewayProbePort:        defaults.GatewayProbePort,
		namespace:               defaults.Namespace,
		gatewayNginxImage:       defaults.GatewayNginxImage,
		gatewayNginxVersion:     defaults.GatewayNginxImageVersion,
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
	defaults.Gateway = opts.gateway
	defaults.GatewayPort = opts.gatewayPort
	defaults.GatewayProbeSeconds = opts.gatewayProbeSeconds
	defaults.GatewayProbePort = opts.gatewayProbePort
	defaults.GatewayNginxImage = opts.gatewayNginxImage
	defaults.GatewayNginxImageVersion = opts.gatewayNginxVersion
	defaults.IdentityTrustDomain = values.GetGlobal().IdentityTrustDomain
	defaults.LinkerdNamespace = controlPlaneNamespace
	defaults.ProxyOutboundPort = uint32(values.GetGlobal().Proxy.Ports.Outbound)
	defaults.LinkerdVersion = version.Version
	defaults.RemoteMirrorServiceAccount = opts.remoteMirrorCredentials

	return defaults, nil
}
