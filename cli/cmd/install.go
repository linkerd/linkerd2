package cmd

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"text/template"

	"github.com/linkerd/linkerd2/cli/install"
	"github.com/linkerd/linkerd2/pkg/k8s"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type installConfig struct {
	Namespace                        string
	ControllerImage                  string
	WebImage                         string
	PrometheusImage                  string
	GrafanaImage                     string
	ControllerReplicas               uint
	WebReplicas                      uint
	PrometheusReplicas               uint
	ImagePullPolicy                  string
	UUID                             string
	CliVersion                       string
	ControllerLogLevel               string
	ControllerComponentLabel         string
	CreatedByAnnotation              string
	ProxyAPIPort                     uint
	EnableTLS                        bool
	TLSTrustAnchorConfigMapName      string
	ProxyContainerName               string
	TLSTrustAnchorFileName           string
	TLSCertFileName                  string
	TLSPrivateKeyFileName            string
	TLSTrustAnchorVolumeSpecFileName string
	TLSIdentityVolumeSpecFileName    string
	InboundPort                      uint
	OutboundPort                     uint
	IgnoreInboundPorts               []uint
	IgnoreOutboundPorts              []uint
	ProxyAutoInjectEnabled           bool
	ProxyAutoInjectLabel             string
	ProxyUID                         int64
	ProxyMetricsPort                 uint
	ProxyControlPort                 uint
	ProxyInjectorTLSSecret           string
	ProxyInjectorSidecarConfig       string
	ProxySpecFileName                string
	ProxyInitSpecFileName            string
	ProxyInitImage                   string
	ProxyImage                       string
	ProxyResourceRequestCPU          string
	ProxyResourceRequestMemory       string
}

type installOptions struct {
	controllerReplicas uint
	webReplicas        uint
	prometheusReplicas uint
	controllerLogLevel string
	proxyAutoInject    bool
	*proxyConfigOptions
}

const prometheusProxyOutboundCapacity = 10000

func newInstallOptions() *installOptions {
	return &installOptions{
		controllerReplicas: 1,
		webReplicas:        1,
		prometheusReplicas: 1,
		controllerLogLevel: "info",
		proxyAutoInject:    false,
		proxyConfigOptions: newProxyConfigOptions(),
	}
}

func newCmdInstall() *cobra.Command {
	options := newInstallOptions()

	cmd := &cobra.Command{
		Use:   "install [flags]",
		Short: "Output Kubernetes configs to install Linkerd",
		Long:  "Output Kubernetes configs to install Linkerd.",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := validateAndBuildConfig(options)
			if err != nil {
				return err
			}

			return render(*config, os.Stdout, options)
		},
	}

	addProxyConfigFlags(cmd, options.proxyConfigOptions)
	cmd.PersistentFlags().UintVar(&options.controllerReplicas, "controller-replicas", options.controllerReplicas, "Replicas of the controller to deploy")
	cmd.PersistentFlags().UintVar(&options.webReplicas, "web-replicas", options.webReplicas, "Replicas of the web server to deploy")
	cmd.PersistentFlags().UintVar(&options.prometheusReplicas, "prometheus-replicas", options.prometheusReplicas, "Replicas of prometheus to deploy")
	cmd.PersistentFlags().StringVar(&options.controllerLogLevel, "controller-log-level", options.controllerLogLevel, "Log level for the controller and web components")
	cmd.PersistentFlags().BoolVar(&options.proxyAutoInject, "proxy-auto-inject", options.proxyAutoInject, "Enable proxy sidecar auto-injection webhook; default to false")

	return cmd
}

func validateAndBuildConfig(options *installOptions) (*installConfig, error) {
	if err := validate(options); err != nil {
		return nil, err
	}
	return &installConfig{
		Namespace:                        controlPlaneNamespace,
		ControllerImage:                  fmt.Sprintf("%s/controller:%s", options.dockerRegistry, options.linkerdVersion),
		WebImage:                         fmt.Sprintf("%s/web:%s", options.dockerRegistry, options.linkerdVersion),
		PrometheusImage:                  "prom/prometheus:v2.4.0",
		GrafanaImage:                     fmt.Sprintf("%s/grafana:%s", options.dockerRegistry, options.linkerdVersion),
		ControllerReplicas:               options.controllerReplicas,
		WebReplicas:                      options.webReplicas,
		PrometheusReplicas:               options.prometheusReplicas,
		ImagePullPolicy:                  options.imagePullPolicy,
		UUID:                             uuid.NewV4().String(),
		CliVersion:                       k8s.CreatedByAnnotationValue(),
		ControllerLogLevel:               options.controllerLogLevel,
		ControllerComponentLabel:         k8s.ControllerComponentLabel,
		CreatedByAnnotation:              k8s.CreatedByAnnotation,
		ProxyAPIPort:                     options.proxyAPIPort,
		EnableTLS:                        options.enableTLS(),
		TLSTrustAnchorConfigMapName:      k8s.TLSTrustAnchorConfigMapName,
		ProxyContainerName:               k8s.ProxyContainerName,
		TLSTrustAnchorFileName:           k8s.TLSTrustAnchorFileName,
		TLSCertFileName:                  k8s.TLSCertFileName,
		TLSPrivateKeyFileName:            k8s.TLSPrivateKeyFileName,
		TLSTrustAnchorVolumeSpecFileName: k8s.TLSTrustAnchorVolumeSpecFileName,
		TLSIdentityVolumeSpecFileName:    k8s.TLSIdentityVolumeSpecFileName,
		InboundPort:                      options.inboundPort,
		OutboundPort:                     options.outboundPort,
		IgnoreInboundPorts:               options.ignoreInboundPorts,
		IgnoreOutboundPorts:              options.ignoreOutboundPorts,
		ProxyAutoInjectEnabled:           options.proxyAutoInject,
		ProxyAutoInjectLabel:             k8s.ProxyAutoInjectLabel,
		ProxyUID:                         options.proxyUID,
		ProxyMetricsPort:                 options.proxyMetricsPort,
		ProxyControlPort:                 options.proxyControlPort,
		ProxyInjectorTLSSecret:           k8s.ProxyInjectorTLSSecret,
		ProxyInjectorSidecarConfig:       k8s.ProxyInjectorSidecarConfig,
		ProxySpecFileName:                k8s.ProxySpecFileName,
		ProxyInitSpecFileName:            k8s.ProxyInitSpecFileName,
		ProxyInitImage:                   options.taggedProxyInitImage(),
		ProxyImage:                       options.taggedProxyImage(),
		ProxyResourceRequestCPU:          options.proxyCpuRequest,
		ProxyResourceRequestMemory:       options.proxyMemoryRequest,
	}, nil
}

func render(config installConfig, w io.Writer, options *installOptions) error {
	template, err := template.New("linkerd").Parse(install.Template)
	if err != nil {
		return err
	}
	buf := &bytes.Buffer{}
	err = template.Execute(buf, config)
	if err != nil {
		return err
	}
	if config.EnableTLS {
		tlsTemplate, err := template.New("linkerd").Parse(install.TlsTemplate)
		if err != nil {
			return err
		}
		err = tlsTemplate.Execute(buf, config)
		if err != nil {
			return err
		}

		if config.ProxyAutoInjectEnabled {
			proxyInjectorTemplate, err := template.New("linkerd").Parse(install.ProxyInjectorTemplate)
			if err != nil {
				return err
			}
			err = proxyInjectorTemplate.Execute(buf, config)
			if err != nil {
				return err
			}
		}
	}

	injectOptions := newInjectOptions()
	injectOptions.proxyConfigOptions = options.proxyConfigOptions

	// Special case for linkerd-proxy running in the Prometheus pod.
	injectOptions.proxyOutboundCapacity[config.PrometheusImage] = prometheusProxyOutboundCapacity

	return InjectYAML(buf, w, ioutil.Discard, injectOptions)
}

func validate(options *installOptions) error {
	if _, err := log.ParseLevel(options.controllerLogLevel); err != nil {
		return fmt.Errorf("--controller-log-level must be one of: panic, fatal, error, warn, info, debug")
	}
	return options.validate()
}
