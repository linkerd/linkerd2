package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"time"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/ptypes"
	"github.com/linkerd/linkerd2/cli/static"
	"github.com/linkerd/linkerd2/controller/gen/config"
	pb "github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/renderutil"
	"k8s.io/helm/pkg/timeconv"
	"sigs.k8s.io/yaml"
)

type (
	installConfig struct {
		Namespace                string
		ControllerImage          string
		WebImage                 string
		PrometheusImage          string
		PrometheusVolumeName     string
		GrafanaImage             string
		GrafanaVolumeName        string
		ControllerReplicas       uint
		ImagePullPolicy          string
		UUID                     string
		CliVersion               string
		ControllerLogLevel       string
		PrometheusLogLevel       string
		ControllerComponentLabel string
		CreatedByAnnotation      string
		ProxyContainerName       string
		ProxyAutoInjectEnabled   bool
		ProxyInjectAnnotation    string
		ProxyInjectDisabled      string
		EnableHA                 bool
		ControllerUID            int64
		EnableH2Upgrade          bool
		NoInitContainer          bool
		GlobalConfig             string
		ProxyConfig              string

		Identity *installIdentityConfig
	}

	installIdentityConfig struct {
		Replicas uint

		TrustDomain     string
		TrustAnchorsPEM string

		Issuer *issuerConfig
	}

	issuerConfig struct {
		ClockSkewAllowance string
		IssuanceLifetime   string

		KeyPEM, CrtPEM string

		CrtExpiry time.Time

		CrtExpiryAnnotation string
	}

	// installOptions holds values for command line flags that apply to the install
	// command. All fields in this struct should have corresponding flags added in
	// the newCmdInstall func later in this file. It also embeds proxyConfigOptions
	// in order to hold values for command line flags that apply to both inject and
	// install.
	installOptions struct {
		controllerReplicas uint
		controllerLogLevel string
		proxyAutoInject    bool
		highAvailability   bool
		controllerUID      int64
		disableH2Upgrade   bool
		identityOptions    *installIdentityOptions
		*proxyConfigOptions
	}

	installIdentityOptions struct {
		trustDomain string

		issuanceLifetime   time.Duration
		clockSkewAllowance time.Duration

		trustPEMFile, crtPEMFile, keyPEMFile string
	}
)

const (
	prometheusProxyOutboundCapacity   = 10000
	defaultControllerReplicas         = 1
	defaultHAControllerReplicas       = 3
	defaultIdentityTrustDomain        = "cluster.local"
	defaultIdentityIssuanceLifetime   = 24 * time.Hour
	defaultIdentityClockSkewAllowance = 20 * time.Second

	nsTemplateName             = "templates/namespace.yaml"
	identityTemplateName       = "templates/identity.yaml"
	controllerTemplateName     = "templates/controller.yaml"
	webTemplateName            = "templates/web.yaml"
	prometheusTemplateName     = "templates/prometheus.yaml"
	grafanaTemplateName        = "templates/grafana.yaml"
	serviceprofileTemplateName = "templates/serviceprofile.yaml"
	proxyInjectorTemplateName  = "templates/proxy_injector.yaml"
)

func newInstallOptions() *installOptions {
	return &installOptions{
		controllerReplicas: defaultControllerReplicas,
		controllerLogLevel: "info",
		proxyAutoInject:    false,
		highAvailability:   false,
		controllerUID:      2103,
		disableH2Upgrade:   false,
		proxyConfigOptions: newProxyConfigOptions(),
		identityOptions: &installIdentityOptions{
			trustDomain:        defaultIdentityTrustDomain,
			issuanceLifetime:   defaultIdentityIssuanceLifetime,
			clockSkewAllowance: defaultIdentityClockSkewAllowance,
		},
	}
}

func newCmdInstall() *cobra.Command {
	options := newInstallOptions()

	cmd := &cobra.Command{
		Use:   "install [flags]",
		Short: "Output Kubernetes configs to install Linkerd",
		Long:  "Output Kubernetes configs to install Linkerd.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO check with a config already exists in the API and fail if it does.

			config, err := validateAndBuildConfig(options)
			if err != nil {
				return err
			}

			return render(*config, os.Stdout, options)
		},
	}

	addProxyConfigFlags(cmd, options.proxyConfigOptions)
	cmd.PersistentFlags().UintVar(
		&options.controllerReplicas, "controller-replicas", options.controllerReplicas,
		"Replicas of the controller to deploy",
	)
	cmd.PersistentFlags().StringVar(
		&options.controllerLogLevel, "controller-log-level", options.controllerLogLevel,
		"Log level for the controller and web components",
	)
	cmd.PersistentFlags().BoolVar(
		&options.proxyAutoInject, "proxy-auto-inject", options.proxyAutoInject,
		"Enable proxy sidecar auto-injection via a webhook (default false)",
	)
	cmd.PersistentFlags().BoolVar(
		&options.highAvailability, "ha", options.highAvailability,
		"Experimental: Enable HA deployment config for the control plane (default false)",
	)
	cmd.PersistentFlags().Int64Var(
		&options.controllerUID, "controller-uid", options.controllerUID,
		"Run the control plane components under this user ID",
	)
	cmd.PersistentFlags().BoolVar(
		&options.disableH2Upgrade, "disable-h2-upgrade", options.disableH2Upgrade,
		"Prevents the controller from instructing proxies to perform transparent HTTP/2 upgrading (default false)",
	)

	cmd.PersistentFlags().StringVar(
		&options.identityOptions.trustDomain, "identity-trust-domain", options.identityOptions.trustDomain,
		"Configures the name suffix used for identities.",
	)
	cmd.PersistentFlags().StringVar(
		&options.identityOptions.trustPEMFile, "identity-trust-anchors-file", options.identityOptions.trustPEMFile,
		"A path to a PEM-encoded file containing Linkerd Identity trust anchors (generated by default)",
	)
	cmd.PersistentFlags().StringVar(
		&options.identityOptions.crtPEMFile, "identity-issuer-certificate-file", options.identityOptions.crtPEMFile,
		"A path to a PEM-encoded file containing the Linkerd Identity issuer certificate (generated by default)",
	)
	cmd.PersistentFlags().StringVar(
		&options.identityOptions.keyPEMFile, "identity-issuer-key-file", options.identityOptions.keyPEMFile,
		"A path to a PEM-encoded file containing the Linkerd Identity issuer private key (generated by default)",
	)
	cmd.PersistentFlags().DurationVar(
		&options.identityOptions.clockSkewAllowance, "identity-clock-skew-allowance", options.identityOptions.clockSkewAllowance,
		"The amount of time to allow for clock skew within a Linkerd cluster",
	)
	cmd.PersistentFlags().DurationVar(
		&options.identityOptions.issuanceLifetime, "identity-issuance-lifetime", options.identityOptions.issuanceLifetime,
		"The amount of time for which the Identity issuer should certify identity",
	)

	return cmd
}

func validateAndBuildConfig(options *installOptions) (*installConfig, error) {
	if err := options.validate(); err != nil {
		return nil, err
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

	var identity *installIdentityConfig
	if idopts := options.identityOptions; idopts != nil {
		trustDomain := idopts.trustDomain
		if trustDomain == "" {
			return nil, errors.New("Trust domain must be specified")
		}
		issuerName := fmt.Sprintf("identity.%s.%s", controlPlaneNamespace, trustDomain)

		identityReplicas := uint(1)
		if options.highAvailability {
			identityReplicas = 3
		}

		// Load signing material from options...
		if idopts.trustPEMFile != "" || idopts.crtPEMFile != "" || idopts.keyPEMFile != "" {
			if idopts.trustPEMFile == "" {
				return nil, errors.New("a trust anchors file must be specified if other credentials are provided")
			}
			if idopts.crtPEMFile == "" {
				return nil, errors.New("a certificate file must be specified if other credentials are provided")
			}
			if idopts.keyPEMFile == "" {
				return nil, errors.New("a private key file must be specified if other credentials are provided")
			}

			// Validate credentials...
			creds, err := tls.ReadPEMCreds(idopts.keyPEMFile, idopts.crtPEMFile)
			if err != nil {
				return nil, err
			}

			trustb, err := ioutil.ReadFile(idopts.trustPEMFile)
			if err != nil {
				return nil, err
			}
			trustAnchorsPEM := string(trustb)
			roots, err := tls.DecodePEMCertPool(trustAnchorsPEM)
			if err != nil {
				return nil, err
			}

			issuerName := "" // TODO restrict issuer name?
			if err := creds.Verify(roots, issuerName); err != nil {
				return nil, fmt.Errorf("Credentials cannot be validated: %s", err)
			}

			identity = &installIdentityConfig{
				Replicas:        identityReplicas,
				TrustDomain:     idopts.trustDomain,
				TrustAnchorsPEM: trustAnchorsPEM,
				Issuer: &issuerConfig{
					ClockSkewAllowance:  idopts.clockSkewAllowance.String(),
					IssuanceLifetime:    idopts.issuanceLifetime.String(),
					CrtExpiryAnnotation: k8s.IdentityIssuerExpiryAnnotation,

					KeyPEM:    creds.EncodePrivateKeyPEM(),
					CrtPEM:    creds.EncodeCertificatePEM(),
					CrtExpiry: creds.Crt.Certificate.NotAfter,
				},
			}
		} else {
			// Generate new signing material...

			root, err := tls.GenerateRootCAWithDefaults(issuerName)
			if err != nil {
				return nil, fmt.Errorf("Failed to create root certificate for identity: %s", err)
			}

			identity = &installIdentityConfig{
				Replicas:        identityReplicas,
				TrustDomain:     trustDomain,
				TrustAnchorsPEM: root.Cred.Crt.EncodeCertificatePEM(),
				Issuer: &issuerConfig{
					ClockSkewAllowance:  idopts.clockSkewAllowance.String(),
					IssuanceLifetime:    idopts.issuanceLifetime.String(),
					CrtExpiryAnnotation: k8s.IdentityIssuerExpiryAnnotation,

					KeyPEM:    root.Cred.EncodePrivateKeyPEM(),
					CrtPEM:    root.Cred.Crt.EncodeCertificatePEM(),
					CrtExpiry: root.Cred.Crt.Certificate.NotAfter,
				},
			}
		}
	}

	jsonMarshaler := jsonpb.Marshaler{EmitDefaults: true}
	globalConfig, err := jsonMarshaler.MarshalToString(globalConfig(options, identity))
	if err != nil {
		return nil, err
	}

	proxyConfig, err := jsonMarshaler.MarshalToString(proxyConfig(options))
	if err != nil {
		return nil, err
	}

	prometheusLogLevel := options.controllerLogLevel
	if prometheusLogLevel == "panic" || prometheusLogLevel == "fatal" {
		prometheusLogLevel = "error"
	}

	return &installConfig{
		Namespace:                controlPlaneNamespace,
		ControllerImage:          fmt.Sprintf("%s/controller:%s", options.dockerRegistry, options.linkerdVersion),
		WebImage:                 fmt.Sprintf("%s/web:%s", options.dockerRegistry, options.linkerdVersion),
		PrometheusImage:          "prom/prometheus:v2.7.1",
		PrometheusVolumeName:     "data",
		GrafanaImage:             fmt.Sprintf("%s/grafana:%s", options.dockerRegistry, options.linkerdVersion),
		GrafanaVolumeName:        "data",
		ControllerReplicas:       options.controllerReplicas,
		ImagePullPolicy:          options.imagePullPolicy,
		UUID:                     uuid.NewV4().String(),
		CliVersion:               k8s.CreatedByAnnotationValue(),
		ControllerLogLevel:       options.controllerLogLevel,
		PrometheusLogLevel:       prometheusLogLevel,
		ControllerComponentLabel: k8s.ControllerComponentLabel,
		ControllerUID:            options.controllerUID,
		CreatedByAnnotation:      k8s.CreatedByAnnotation,
		ProxyContainerName:       k8s.ProxyContainerName,
		ProxyAutoInjectEnabled:   options.proxyAutoInject,
		ProxyInjectAnnotation:    k8s.ProxyInjectAnnotation,
		ProxyInjectDisabled:      k8s.ProxyInjectDisabled,
		EnableHA:                 options.highAvailability,
		EnableH2Upgrade:          !options.disableH2Upgrade,
		NoInitContainer:          options.noInitContainer,
		GlobalConfig:             globalConfig,
		ProxyConfig:              proxyConfig,
		Identity:                 identity,
	}, nil
}

func render(config installConfig, w io.Writer, options *installOptions) error {
	// Render raw values and create chart config
	rawValues, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	chrtConfig := &chart.Config{Raw: string(rawValues), Values: map[string]*chart.Value{}}

	files := []*chartutil.BufferedFile{
		{Name: chartutil.ChartfileName},
		{Name: nsTemplateName},
		{Name: identityTemplateName},
		{Name: controllerTemplateName},
		{Name: serviceprofileTemplateName},
		{Name: webTemplateName},
		{Name: prometheusTemplateName},
		{Name: grafanaTemplateName},
		{Name: proxyInjectorTemplateName},
	}

	// Read templates into bytes
	for _, f := range files {
		data, err := readIntoBytes(f.Name)
		if err != nil {
			return err
		}
		f.Data = data
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
	for _, tmpl := range files {
		t := path.Join(renderOpts.ReleaseOptions.Name, tmpl.Name)
		if _, err := buf.WriteString(renderedTemplates[t]); err != nil {
			return err
		}
	}

	injectOptions := newInjectOptions()

	*injectOptions.proxyConfigOptions = *options.proxyConfigOptions

	// Skip outbound port 443 to enable Kubernetes API access without the proxy.
	// Once Kubernetes supports sidecar containers, this may be removed, as that
	// will guarantee the proxy is running prior to control-plane startup.
	injectOptions.ignoreOutboundPorts = append(injectOptions.ignoreOutboundPorts, 443)

	// TODO: Fetch GlobalConfig and ProxyConfig from the ConfigMap/API
	pbConfig := injectOptionsToConfigs(injectOptions)

	// injectOptionsToConfigs does NOT set an identity context if none exists,
	// since it can't be enabled at inject-time if it's not enabled at
	// install-time.
	pbConfig.global.IdentityContext = config.Identity.toIdentityContext()

	return processYAML(&buf, w, ioutil.Discard, resourceTransformerInject{
		configs: pbConfig,
		proxyOutboundCapacity: map[string]uint{
			config.PrometheusImage: prometheusProxyOutboundCapacity,
		},
	})
}

func (options *installOptions) validate() error {
	if _, err := log.ParseLevel(options.controllerLogLevel); err != nil {
		return fmt.Errorf("--controller-log-level must be one of: panic, fatal, error, warn, info, debug")
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

func globalConfig(options *installOptions, id *installIdentityConfig) *pb.Global {
	return &pb.Global{
		LinkerdNamespace: controlPlaneNamespace,
		CniEnabled:       options.noInitContainer,
		Version:          options.linkerdVersion,
		IdentityContext:  id.toIdentityContext(),
	}
}

func proxyConfig(options *installOptions) *pb.Proxy {
	ignoreInboundPorts := []*pb.Port{}
	for _, port := range options.ignoreInboundPorts {
		ignoreInboundPorts = append(ignoreInboundPorts, &pb.Port{Port: uint32(port)})
	}

	ignoreOutboundPorts := []*pb.Port{}
	for _, port := range options.ignoreOutboundPorts {
		ignoreOutboundPorts = append(ignoreOutboundPorts, &pb.Port{Port: uint32(port)})
	}

	return &pb.Proxy{
		ProxyImage: &pb.Image{
			ImageName:  registryOverride(options.proxyImage, options.dockerRegistry),
			PullPolicy: options.imagePullPolicy,
		},
		ProxyInitImage: &pb.Image{
			ImageName:  registryOverride(options.initImage, options.dockerRegistry),
			PullPolicy: options.imagePullPolicy,
		},
		ControlPort: &pb.Port{
			Port: uint32(options.proxyControlPort),
		},
		IgnoreInboundPorts:  ignoreInboundPorts,
		IgnoreOutboundPorts: ignoreOutboundPorts,
		InboundPort: &pb.Port{
			Port: uint32(options.inboundPort),
		},
		AdminPort: &config.Port{
			Port: uint32(options.proxyAdminPort),
		},
		OutboundPort: &pb.Port{
			Port: uint32(options.outboundPort),
		},
		Resource: &pb.ResourceRequirements{
			RequestCpu:    options.proxyCPURequest,
			RequestMemory: options.proxyMemoryRequest,
			LimitCpu:      options.proxyCPULimit,
			LimitMemory:   options.proxyMemoryLimit,
		},
		ProxyUid: options.proxyUID,
		LogLevel: &pb.LogLevel{
			Level: options.proxyLogLevel,
		},
		DisableExternalProfiles: options.disableExternalProfiles,
	}
}

func (id *installIdentityConfig) toIdentityContext() *pb.IdentityContext {
	if id == nil {
		return nil
	}

	il, err := time.ParseDuration(id.Issuer.IssuanceLifetime)
	if err != nil {
		il = defaultIdentityIssuanceLifetime
	}

	csa, err := time.ParseDuration(id.Issuer.ClockSkewAllowance)
	if err != nil {
		csa = defaultIdentityClockSkewAllowance
	}

	return &pb.IdentityContext{
		TrustDomain:        id.TrustDomain,
		TrustAnchorsPem:    id.TrustAnchorsPEM,
		IssuanceLifetime:   ptypes.DurationProto(il),
		ClockSkewAllowance: ptypes.DurationProto(csa),
	}
}
