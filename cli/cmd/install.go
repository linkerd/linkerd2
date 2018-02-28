package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"text/template"

	"github.com/runconduit/conduit/cli/install"
	"github.com/runconduit/conduit/pkg/k8s"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type installConfig struct {
	Namespace                string
	ControllerImage          string
	WebImage                 string
	PrometheusImage          string
	GrafanaImage             string
	VizDashboard             string
	HealthDashboard          string
	ControllerReplicas       uint
	WebReplicas              uint
	PrometheusReplicas       uint
	ImagePullPolicy          string
	UUID                     string
	CliVersion               string
	ControllerLogLevel       string
	ControllerComponentLabel string
	CreatedByAnnotation      string
}

var (
	conduitVersion     string
	dockerRegistry     string
	controllerReplicas uint
	webReplicas        uint
	prometheusReplicas uint
	imagePullPolicy    string
	controllerLogLevel string
)

var installCmd = &cobra.Command{
	Use:   "install [flags]",
	Short: "Output Kubernetes configs to install Conduit",
	Long:  "Output Kubernetes configs to install Conduit.",
	RunE: func(cmd *cobra.Command, args []string) error {
		config, err := validateAndBuildConfig()
		if err != nil {
			return err
		}
		return render(*config, os.Stdout)
	},
}

func validateAndBuildConfig() (*installConfig, error) {
	if err := validate(); err != nil {
		return nil, err
	}
	return &installConfig{
		Namespace:                controlPlaneNamespace,
		ControllerImage:          fmt.Sprintf("%s/controller:%s", dockerRegistry, conduitVersion),
		WebImage:                 fmt.Sprintf("%s/web:%s", dockerRegistry, conduitVersion),
		PrometheusImage:          "prom/prometheus:v2.1.0",
		GrafanaImage:             "grafana/grafana:5.0.0-beta4",
		VizDashboard:             install.Viz,
		HealthDashboard:          install.Health,
		ControllerReplicas:       controllerReplicas,
		WebReplicas:              webReplicas,
		PrometheusReplicas:       prometheusReplicas,
		ImagePullPolicy:          imagePullPolicy,
		UUID:                     uuid.NewV4().String(),
		CliVersion:               k8s.CreatedByAnnotationValue(),
		ControllerLogLevel:       controllerLogLevel,
		ControllerComponentLabel: k8s.ControllerComponentLabel,
		CreatedByAnnotation:      k8s.CreatedByAnnotation,
	}, nil
}

func render(config installConfig, w io.Writer) error {
	template, err := template.New("conduit").Parse(install.Template)
	if err != nil {
		return err
	}
	buf := &bytes.Buffer{}
	err = template.Execute(buf, config)
	if err != nil {
		return err
	}
	return InjectYAML(buf, w, conduitVersion)
}

var alphaNumDash = regexp.MustCompile("^[a-zA-Z0-9-]+$")
var alphaNumDashDot = regexp.MustCompile("^[\\.a-zA-Z0-9-]+$")
var alphaNumDashDotSlash = regexp.MustCompile("^[\\./a-zA-Z0-9-]+$")

func validate() error {
	// These regexs are not as strict as they could be, but are a quick and dirty
	// sanity check against illegal characters.
	if !alphaNumDash.MatchString(controlPlaneNamespace) {
		return fmt.Errorf("%s is not a valid namespace", controlPlaneNamespace)
	}
	if !alphaNumDashDot.MatchString(conduitVersion) {
		return fmt.Errorf("%s is not a valid version", conduitVersion)
	}
	if !alphaNumDashDotSlash.MatchString(dockerRegistry) {
		return fmt.Errorf("%s is not a valid Docker registry", dockerRegistry)
	}
	if imagePullPolicy != "Always" && imagePullPolicy != "IfNotPresent" && imagePullPolicy != "Never" {
		return fmt.Errorf("--image-pull-policy must be one of: Always, IfNotPresent, Never")
	}
	if _, err := log.ParseLevel(controllerLogLevel); err != nil {
		return fmt.Errorf("--controller-log-level must be one of: panic, fatal, error, warn, info, debug")
	}
	return nil
}

func init() {
	RootCmd.AddCommand(installCmd)
	addProxyConfigFlags(installCmd)
	installCmd.PersistentFlags().StringVarP(&dockerRegistry, "registry", "r", "gcr.io/runconduit", "Docker registry to pull images from")
	installCmd.PersistentFlags().UintVar(&controllerReplicas, "controller-replicas", 1, "replicas of the controller to deploy")
	installCmd.PersistentFlags().UintVar(&webReplicas, "web-replicas", 1, "replicas of the web server to deploy")
	installCmd.PersistentFlags().UintVar(&prometheusReplicas, "prometheus-replicas", 1, "replicas of prometheus to deploy")
	installCmd.PersistentFlags().StringVar(&controllerLogLevel, "controller-log-level", "info", "log level for the controller and web components")
}
