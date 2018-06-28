package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"text/template"

	"github.com/runconduit/conduit/cli/install"
	"github.com/runconduit/conduit/pkg/k8s"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type installConfig struct {
	Namespace                   string
	ControllerImage             string
	WebImage                    string
	PrometheusImage             string
	GrafanaImage                string
	ControllerReplicas          uint
	WebReplicas                 uint
	PrometheusReplicas          uint
	ImagePullPolicy             string
	UUID                        string
	CliVersion                  string
	ControllerLogLevel          string
	ControllerComponentLabel    string
	CreatedByAnnotation         string
	ProxyAPIPort                uint
	EnableTLS                   bool
	TLSTrustAnchorConfigMapName string
}

type installOptions struct {
	controllerReplicas uint
	webReplicas        uint
	prometheusReplicas uint
	controllerLogLevel string
	*proxyConfigOptions
}

func newInstallOptions() *installOptions {
	return &installOptions{
		controllerReplicas: 1,
		webReplicas:        1,
		prometheusReplicas: 1,
		controllerLogLevel: "info",
		proxyConfigOptions: newProxyConfigOptions(),
	}
}

func newCmdInstall() *cobra.Command {
	options := newInstallOptions()

	cmd := &cobra.Command{
		Use:   "install [flags]",
		Short: "Output Kubernetes configs to install Conduit",
		Long:  "Output Kubernetes configs to install Conduit.",
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

	return cmd
}

func validateAndBuildConfig(options *installOptions) (*installConfig, error) {
	if err := validate(options); err != nil {
		return nil, err
	}
	return &installConfig{
		Namespace:                   controlPlaneNamespace,
		ControllerImage:             fmt.Sprintf("%s/controller:%s", options.dockerRegistry, options.conduitVersion),
		WebImage:                    fmt.Sprintf("%s/web:%s", options.dockerRegistry, options.conduitVersion),
		PrometheusImage:             "prom/prometheus:v2.3.1",
		GrafanaImage:                fmt.Sprintf("%s/grafana:%s", options.dockerRegistry, options.conduitVersion),
		ControllerReplicas:          options.controllerReplicas,
		WebReplicas:                 options.webReplicas,
		PrometheusReplicas:          options.prometheusReplicas,
		ImagePullPolicy:             options.imagePullPolicy,
		UUID:                        uuid.NewV4().String(),
		CliVersion:                  k8s.CreatedByAnnotationValue(),
		ControllerLogLevel:          options.controllerLogLevel,
		ControllerComponentLabel:    k8s.ControllerComponentLabel,
		CreatedByAnnotation:         k8s.CreatedByAnnotation,
		ProxyAPIPort:                options.proxyAPIPort,
		EnableTLS:                   options.enableTLS(),
		TLSTrustAnchorConfigMapName: k8s.TLSTrustAnchorConfigMapName,
	}, nil
}

func render(config installConfig, w io.Writer, options *installOptions) error {
	template, err := template.New("conduit").Parse(install.Template)
	if err != nil {
		return err
	}
	buf := &bytes.Buffer{}
	err = template.Execute(buf, config)
	if err != nil {
		return err
	}
	if config.EnableTLS {
		tlsTemplate, err := template.New("conduit").Parse(install.TlsTemplate)
		if err != nil {
			return err
		}
		err = tlsTemplate.Execute(buf, config)
		if err != nil {
			return err
		}
	}
	injectOptions := newInjectOptions()
	injectOptions.proxyConfigOptions = options.proxyConfigOptions
	return InjectYAML(buf, w, injectOptions)
}

func validate(options *installOptions) error {
	if _, err := log.ParseLevel(options.controllerLogLevel); err != nil {
		return fmt.Errorf("--controller-log-level must be one of: panic, fatal, error, warn, info, debug")
	}
	return options.validate()
}
