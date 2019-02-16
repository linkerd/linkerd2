package cmd

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/linkerd/linkerd2/cli/static"
	"github.com/linkerd/linkerd2/pkg/k8s"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/renderutil"
	"k8s.io/helm/pkg/timeconv"
	"sigs.k8s.io/yaml"
)

type installConfig struct {
	Namespace                  string
	ControllerImage            string
	WebImage                   string
	PrometheusImage            string
	PrometheusVolumeName       string
	GrafanaImage               string
	GrafanaVolumeName          string
	ControllerReplicas         uint
	ImagePullPolicy            string
	UUID                       string
	CliVersion                 string
	ControllerLogLevel         string
	ControllerComponentLabel   string
	CreatedByAnnotation        string
	DestinationAPIPort         uint
	ProxyContainerName         string
	InboundPort                uint
	OutboundPort               uint
	IgnoreInboundPorts         string
	IgnoreOutboundPorts        string
	InboundAcceptKeepaliveMs   uint
	OutboundConnectKeepaliveMs uint
	ProxyAutoInjectEnabled     bool
	ProxyInjectAnnotation      string
	ProxyInjectDisabled        string
	ProxyLogLevel              string
	ProxyUID                   int64
	ProxyMetricsPort           uint
	ProxyControlPort           uint
	ProxySpecFileName          string
	ProxyInitSpecFileName      string
	ProxyInitImage             string
	ProxyImage                 string
	ProxyResourceRequestCPU    string
	ProxyResourceRequestMemory string
	SingleNamespace            bool
	EnableHA                   bool
	ControllerUID              int64
	ProfileSuffixes            string
	EnableH2Upgrade            bool
	NoInitContainer            bool
}

// installOptions holds values for command line flags that apply to the install
// command. All fields in this struct should have corresponding flags added in
// the newCmdInstall func later in this file. It also embeds proxyConfigOptions
// in order to hold values for command line flags that apply to both inject and
// install.
type installOptions struct {
	controllerReplicas uint
	controllerLogLevel string
	proxyAutoInject    bool
	singleNamespace    bool
	highAvailability   bool
	controllerUID      int64
	disableH2Upgrade   bool
	*proxyConfigOptions
}

const (
	prometheusProxyOutboundCapacity = 10000
	defaultControllerReplicas       = 1
	defaultHAControllerReplicas     = 3

	baseTemplateName          = "templates/base.yaml"
	proxyInjectorTemplateName = "templates/proxy_injector.yaml"
)

func newInstallOptions() *installOptions {
	return &installOptions{
		controllerReplicas: defaultControllerReplicas,
		controllerLogLevel: "info",
		proxyAutoInject:    false,
		singleNamespace:    false,
		highAvailability:   false,
		controllerUID:      2103,
		disableH2Upgrade:   false,
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
	cmd.PersistentFlags().StringVar(&options.controllerLogLevel, "controller-log-level", options.controllerLogLevel, "Log level for the controller and web components")
	cmd.PersistentFlags().BoolVar(&options.proxyAutoInject, "proxy-auto-inject", options.proxyAutoInject, "Experimental: Enable proxy sidecar auto-injection webhook (default false)")
	cmd.PersistentFlags().BoolVar(&options.singleNamespace, "single-namespace", options.singleNamespace, "Experimental: Configure the control plane to only operate in the installed namespace (default false)")
	cmd.PersistentFlags().BoolVar(&options.highAvailability, "ha", options.highAvailability, "Experimental: Enable HA deployment config for the control plane")
	cmd.PersistentFlags().Int64Var(&options.controllerUID, "controller-uid", options.controllerUID, "Run the control plane components under this user ID")
	cmd.PersistentFlags().BoolVar(&options.disableH2Upgrade, "disable-h2-upgrade", options.disableH2Upgrade, "Prevents the controller from instructing proxies to perform transparent HTTP/2 upgrading")
	return cmd
}

func validateAndBuildConfig(options *installOptions) (*installConfig, error) {
	if err := options.validate(); err != nil {
		return nil, err
	}

	ignoreInboundPorts := []string{
		fmt.Sprintf("%d", options.proxyControlPort),
		fmt.Sprintf("%d", options.proxyMetricsPort),
	}
	for _, p := range options.ignoreInboundPorts {
		ignoreInboundPorts = append(ignoreInboundPorts, fmt.Sprintf("%d", p))
	}
	ignoreOutboundPorts := []string{}
	for _, p := range options.ignoreOutboundPorts {
		ignoreOutboundPorts = append(ignoreOutboundPorts, fmt.Sprintf("%d", p))
	}

	if options.highAvailability && options.controllerReplicas == defaultControllerReplicas {
		options.controllerReplicas = defaultHAControllerReplicas
	}

	if options.highAvailability && options.proxyCPURequest == "" {
		options.proxyCPURequest = "10m"
	}

	if options.highAvailability && options.proxyMemoryRequest == "" {
		options.proxyMemoryRequest = "20Mi"
	}

	profileSuffixes := "."
	if options.proxyConfigOptions.disableExternalProfiles {
		profileSuffixes = "svc.cluster.local."
	}

	return &installConfig{
		Namespace:                  controlPlaneNamespace,
		ControllerImage:            fmt.Sprintf("%s/controller:%s", options.dockerRegistry, options.linkerdVersion),
		WebImage:                   fmt.Sprintf("%s/web:%s", options.dockerRegistry, options.linkerdVersion),
		PrometheusImage:            "prom/prometheus:v2.7.1",
		PrometheusVolumeName:       "data",
		GrafanaImage:               fmt.Sprintf("%s/grafana:%s", options.dockerRegistry, options.linkerdVersion),
		GrafanaVolumeName:          "data",
		ControllerReplicas:         options.controllerReplicas,
		ImagePullPolicy:            options.imagePullPolicy,
		UUID:                       uuid.NewV4().String(),
		CliVersion:                 k8s.CreatedByAnnotationValue(),
		ControllerLogLevel:         options.controllerLogLevel,
		ControllerComponentLabel:   k8s.ControllerComponentLabel,
		ControllerUID:              options.controllerUID,
		CreatedByAnnotation:        k8s.CreatedByAnnotation,
		DestinationAPIPort:         options.destinationAPIPort,
		ProxyContainerName:         k8s.ProxyContainerName,
		InboundPort:                options.inboundPort,
		OutboundPort:               options.outboundPort,
		IgnoreInboundPorts:         strings.Join(ignoreInboundPorts, ","),
		IgnoreOutboundPorts:        strings.Join(ignoreOutboundPorts, ","),
		InboundAcceptKeepaliveMs:   defaultKeepaliveMs,
		OutboundConnectKeepaliveMs: defaultKeepaliveMs,
		ProxyAutoInjectEnabled:     options.proxyAutoInject,
		ProxyInjectAnnotation:      k8s.ProxyInjectAnnotation,
		ProxyInjectDisabled:        k8s.ProxyInjectDisabled,
		ProxyLogLevel:              options.proxyLogLevel,
		ProxyUID:                   options.proxyUID,
		ProxyMetricsPort:           options.proxyMetricsPort,
		ProxyControlPort:           options.proxyControlPort,
		ProxySpecFileName:          k8s.ProxySpecFileName,
		ProxyInitSpecFileName:      k8s.ProxyInitSpecFileName,
		ProxyInitImage:             options.taggedProxyInitImage(),
		ProxyImage:                 options.taggedProxyImage(),
		ProxyResourceRequestCPU:    options.proxyCPURequest,
		ProxyResourceRequestMemory: options.proxyMemoryRequest,
		SingleNamespace:            options.singleNamespace,
		EnableHA:                   options.highAvailability,
		ProfileSuffixes:            profileSuffixes,
		EnableH2Upgrade:            !options.disableH2Upgrade,
		NoInitContainer:            options.noInitContainer,
	}, nil
}

func render(config installConfig, w io.Writer, options *installOptions) error {
	// Render raw values and create chart config
	rawValues, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	chrtConfig := &chart.Config{Raw: string(rawValues), Values: map[string]*chart.Value{}}

	// Read templates into bytes
	chartTmpl, err := readIntoBytes(chartutil.ChartfileName)
	if err != nil {
		return err
	}
	baseTmpl, err := readIntoBytes(baseTemplateName)
	if err != nil {
		return err
	}
	proxyInjectorTmpl, err := readIntoBytes(proxyInjectorTemplateName)
	if err != nil {
		return err
	}

	files := []*chartutil.BufferedFile{
		{Name: chartutil.ChartfileName, Data: chartTmpl},
		{Name: baseTemplateName, Data: baseTmpl},
		{Name: proxyInjectorTemplateName, Data: proxyInjectorTmpl},
	}

	// Create chart and render templates
	chrt, err := chartutil.LoadFiles(files)
	if err != nil {
		return err
	}

	renderOpts := renderutil.Options{
		ReleaseOptions: chartutil.ReleaseOptions{
			Name:      "linkerd",
			IsInstall: true,
			IsUpgrade: false,
			Time:      timeconv.Now(),
			Namespace: controlPlaneNamespace,
		},
		KubeVersion: "",
	}

	renderedTemplates, err := renderutil.Render(chrt, chrtConfig, renderOpts)
	if err != nil {
		return err
	}

	// Merge templates and inject
	var buf bytes.Buffer
	bt := path.Join(renderOpts.ReleaseOptions.Name, baseTemplateName)
	if _, err := buf.WriteString(renderedTemplates[bt]); err != nil {
		return err
	}

	if config.ProxyAutoInjectEnabled {
		pt := path.Join(renderOpts.ReleaseOptions.Name, proxyInjectorTemplateName)
		if _, err := buf.WriteString(renderedTemplates[pt]); err != nil {
			return err
		}
	}

	injectOptions := newInjectOptions()
	injectOptions.proxyConfigOptions = options.proxyConfigOptions

	// Special case for linkerd-proxy running in the Prometheus pod.
	injectOptions.proxyOutboundCapacity[config.PrometheusImage] = prometheusProxyOutboundCapacity

	return InjectYAML(&buf, w, ioutil.Discard, injectOptions)
}

func (options *installOptions) validate() error {
	if _, err := log.ParseLevel(options.controllerLogLevel); err != nil {
		return fmt.Errorf("--controller-log-level must be one of: panic, fatal, error, warn, info, debug")
	}

	if options.proxyAutoInject && options.singleNamespace {
		return fmt.Errorf("The --proxy-auto-inject and --single-namespace flags cannot both be specified together")
	}

	return options.proxyConfigOptions.validate()
}

func readIntoBytes(filename string) ([]byte, error) {
	file, err := static.Templates.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(file)

	return buf.Bytes(), nil
}
