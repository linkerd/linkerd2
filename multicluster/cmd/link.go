package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/linkerd/linkerd2/multicluster/static"
	multicluster "github.com/linkerd/linkerd2/multicluster/values"
	"github.com/linkerd/linkerd2/pkg/charts"
	partials "github.com/linkerd/linkerd2/pkg/charts/static"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/k8s"
	mc "github.com/linkerd/linkerd2/pkg/multicluster"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	chartloader "helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	valuespkg "helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/engine"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/yaml"
)

type (
	linkOptions struct {
		namespace               string
		clusterName             string
		apiServerAddress        string
		serviceAccountName      string
		gatewayName             string
		gatewayNamespace        string
		serviceMirrorRetryLimit uint32
		logLevel                string
		controlPlaneVersion     string
		dockerRegistry          string
		selector                string
		gatewayAddresses        string
		gatewayPort             uint32
	}
)

func newLinkCommand() *cobra.Command {
	opts, err := newLinkOptionsWithDefault()
	var valuesOptions valuespkg.Options

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cmd := &cobra.Command{
		Use:   "link",
		Short: "Outputs resources that allow another cluster to mirror services from this one",
		Long: `Outputs resources that allow another cluster to mirror services from this one.

Note that the Link resource applies only in one direction. In order for two
clusters to mirror each other, a Link resource will have to be generated for
each cluster and applied to the other.`,
		Args: cobra.NoArgs,
		Example: `  # To link the west cluster to east
  linkerd --context=east multicluster link --cluster-name east | kubectl --context=west apply -f -

The command can be configured by using the --set, --values, --set-string and --set-file flags.
A full list of configurable values can be found at https://github.com/linkerd/linkerd2/blob/main/multicluster/charts/linkerd-multicluster-link/README.md
  `,
		RunE: func(cmd *cobra.Command, args []string) error {

			if opts.clusterName == "" {
				return errors.New("You need to specify cluster name")
			}

			configMap, err := getLinkerdConfigMap(cmd.Context())
			if err != nil {
				if kerrors.IsNotFound(err) {
					return errors.New("you need Linkerd to be installed on a cluster in order to get its credentials")
				}
				return err
			}

			rules := clientcmd.NewDefaultClientConfigLoadingRules()
			rules.ExplicitPath = kubeconfigPath
			loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
			config, err := loader.RawConfig()
			if err != nil {
				return err
			}

			if kubeContext != "" {
				config.CurrentContext = kubeContext
			}

			k, err := k8s.NewAPI(kubeconfigPath, config.CurrentContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			sa, err := k.CoreV1().ServiceAccounts(opts.namespace).Get(cmd.Context(), opts.serviceAccountName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			listOpts := metav1.ListOptions{
				FieldSelector: fmt.Sprintf("type=%s", corev1.SecretTypeServiceAccountToken),
			}
			secrets, err := k.CoreV1().Secrets(opts.namespace).List(cmd.Context(), listOpts)
			if err != nil {
				return err
			}

			token, err := extractSAToken(secrets.Items, sa.Name)
			if err != nil {
				return err
			}

			context, ok := config.Contexts[config.CurrentContext]
			if !ok {
				return fmt.Errorf("could not extract current context from config")
			}

			context.AuthInfo = opts.serviceAccountName
			config.Contexts = map[string]*api.Context{
				config.CurrentContext: context,
			}
			config.AuthInfos = map[string]*api.AuthInfo{
				opts.serviceAccountName: {
					Token: token,
				},
			}

			cluster := config.Clusters[context.Cluster]

			if opts.apiServerAddress != "" {
				cluster.Server = opts.apiServerAddress
			}

			config.Clusters = map[string]*api.Cluster{
				context.Cluster: cluster,
			}

			kubeconfig, err := clientcmd.Write(config)
			if err != nil {
				return err
			}

			creds := corev1.Secret{
				Type:     k8s.MirrorSecretType,
				TypeMeta: metav1.TypeMeta{Kind: "Secret", APIVersion: "v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("cluster-credentials-%s", opts.clusterName),
					Namespace: opts.namespace,
				},
				Data: map[string][]byte{
					k8s.ConfigKeyName: kubeconfig,
				},
			}

			credsOut, err := yaml.Marshal(creds)
			if err != nil {
				return err
			}

			gateway, err := k.CoreV1().Services(opts.gatewayNamespace).Get(cmd.Context(), opts.gatewayName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			gatewayAddresses := ""
			gwAddresses := []string{}
			for _, ingress := range gateway.Status.LoadBalancer.Ingress {
				addr := ingress.IP
				if addr == "" {
					addr = ingress.Hostname
				}
				if addr == "" {
					continue
				}
				gwAddresses = append(gwAddresses, addr)
			}
			if len(gwAddresses) == 0 && opts.gatewayAddresses == "" {
				return fmt.Errorf("Gateway %s.%s has no ingress addresses", gateway.Name, gateway.Namespace)
			}
			if len(gwAddresses) > 0 {
				gatewayAddresses = strings.Join(gwAddresses, ",")
			} else {
				gatewayAddresses = opts.gatewayAddresses
			}

			gatewayIdentity, ok := gateway.Annotations[k8s.GatewayIdentity]
			if !ok || gatewayIdentity == "" {
				return fmt.Errorf("Gateway %s.%s has no %s annotation", gateway.Name, gateway.Namespace, k8s.GatewayIdentity)
			}

			probeSpec, err := mc.ExtractProbeSpec(gateway)
			if err != nil {
				return err
			}

			gatewayPort, err := extractGatewayPort(gateway)
			if err != nil {
				return err
			}

			// Override with user provided gateway port if present
			if opts.gatewayPort != 0 {
				gatewayPort = opts.gatewayPort
			}

			selector, err := metav1.ParseToLabelSelector(opts.selector)
			if err != nil {
				return err
			}

			link := mc.Link{
				Name:                          opts.clusterName,
				Namespace:                     opts.namespace,
				TargetClusterName:             opts.clusterName,
				TargetClusterDomain:           configMap.ClusterDomain,
				TargetClusterLinkerdNamespace: controlPlaneNamespace,
				ClusterCredentialsSecret:      fmt.Sprintf("cluster-credentials-%s", opts.clusterName),
				GatewayAddress:                gatewayAddresses,
				GatewayPort:                   gatewayPort,
				GatewayIdentity:               gatewayIdentity,
				ProbeSpec:                     probeSpec,
				Selector:                      *selector,
			}

			obj, err := link.ToUnstructured()
			if err != nil {
				return err
			}
			linkOut, err := yaml.Marshal(obj.Object)
			if err != nil {
				return err
			}

			values, err := buildServiceMirrorValues(opts)
			if err != nil {
				return err
			}

			// Create values override
			valuesOverrides, err := valuesOptions.MergeValues(nil)
			if err != nil {
				return err
			}

			serviceMirrorOut, err := renderServiceMirror(values, valuesOverrides, opts.namespace)
			if err != nil {
				return err
			}

			stdout.Write(credsOut)
			stdout.Write([]byte("---\n"))
			stdout.Write(linkOut)
			stdout.Write([]byte("---\n"))
			stdout.Write(serviceMirrorOut)
			stdout.Write([]byte("---\n"))

			return nil
		},
	}

	flags.AddValueOptionsFlags(cmd.Flags(), &valuesOptions)
	cmd.Flags().StringVar(&opts.namespace, "namespace", defaultMulticlusterNamespace, "The namespace for the service account")
	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", "", "Cluster name")
	cmd.Flags().StringVar(&opts.apiServerAddress, "api-server-address", "", "The api server address of the target cluster")
	cmd.Flags().StringVar(&opts.serviceAccountName, "service-account-name", defaultServiceAccountName, "The name of the service account associated with the credentials")
	cmd.Flags().StringVar(&opts.controlPlaneVersion, "control-plane-version", opts.controlPlaneVersion, "(Development) Tag to be used for the service mirror controller image")
	cmd.Flags().StringVar(&opts.gatewayName, "gateway-name", defaultGatewayName, "The name of the gateway service")
	cmd.Flags().StringVar(&opts.gatewayNamespace, "gateway-namespace", defaultMulticlusterNamespace, "The namespace of the gateway service")
	cmd.Flags().Uint32Var(&opts.serviceMirrorRetryLimit, "service-mirror-retry-limit", opts.serviceMirrorRetryLimit, "The number of times a failed update from the target cluster is allowed to be retried")
	cmd.Flags().StringVar(&opts.logLevel, "log-level", opts.logLevel, "Log level for the Multicluster components")
	cmd.Flags().StringVar(&opts.dockerRegistry, "registry", opts.dockerRegistry,
		fmt.Sprintf("Docker registry to pull service mirror controller image from ($%s)", flags.EnvOverrideDockerRegistry))
	cmd.Flags().StringVarP(&opts.selector, "selector", "l", opts.selector, "Selector (label query) to filter which services in the target cluster to mirror")
	cmd.Flags().StringVar(&opts.gatewayAddresses, "gateway-addresses", opts.gatewayAddresses, "If specified, overwrites gateway addresses when gateway service is not type LoadBalancer (comma separated list)")
	cmd.Flags().Uint32Var(&opts.gatewayPort, "gateway-port", opts.gatewayPort, "If specified, overwrites gateway port when gateway service is not type LoadBalancer")

	pkgcmd.ConfigureNamespaceFlagCompletion(
		cmd, []string{"namespace", "gateway-namespace"},
		kubeconfigPath, impersonate, impersonateGroup, kubeContext)
	return cmd
}

func renderServiceMirror(values *multicluster.Values, valuesOverrides map[string]interface{}, namespace string) ([]byte, error) {
	files := []*chartloader.BufferedFile{
		{Name: chartutil.ChartfileName},
		{Name: "templates/service-mirror.yaml"},
		{Name: "templates/psp.yaml"},
		{Name: "templates/gateway-mirror.yaml"},
	}

	var partialFiles []*chartloader.BufferedFile
	for _, template := range charts.L5dPartials {
		partialFiles = append(partialFiles,
			&chartloader.BufferedFile{Name: template},
		)
	}

	// Load all multicluster link chart files into buffer
	if err := charts.FilesReader(static.Templates, helmMulticlusterLinkDefaultChartName+"/", files); err != nil {
		return nil, err
	}

	// Load all partial chart files into buffer
	if err := charts.FilesReader(partials.Templates, "", partialFiles); err != nil {
		return nil, err
	}

	// Create a Chart obj from the files
	chart, err := chartloader.LoadFiles(append(files, partialFiles...))
	if err != nil {
		return nil, err
	}

	// Render raw values and create chart config
	rawValues, err := yaml.Marshal(values)
	if err != nil {
		return nil, err
	}

	// Store final Values generated from values.yaml and CLI flags
	err = yaml.Unmarshal(rawValues, &chart.Values)
	if err != nil {
		return nil, err
	}

	vals, err := chartutil.CoalesceValues(chart, valuesOverrides)
	if err != nil {
		return nil, err
	}

	fullValues := map[string]interface{}{
		"Values": vals,
		"Release": map[string]interface{}{
			"Namespace": namespace,
			"Service":   "CLI",
		},
	}

	// Attach the final values into the `Values` field for rendering to work
	renderedTemplates, err := engine.Render(chart, fullValues)
	if err != nil {
		return nil, err
	}

	// Merge templates and inject
	var out bytes.Buffer
	for _, tmpl := range chart.Templates {
		t := path.Join(chart.Metadata.Name, tmpl.Name)
		if _, err := out.WriteString(renderedTemplates[t]); err != nil {
			return nil, err
		}
	}

	return out.Bytes(), nil
}

func newLinkOptionsWithDefault() (*linkOptions, error) {
	defaults, err := multicluster.NewLinkValues()
	if err != nil {
		return nil, err
	}

	return &linkOptions{
		controlPlaneVersion:     version.Version,
		namespace:               defaultMulticlusterNamespace,
		dockerRegistry:          defaultDockerRegistry,
		serviceMirrorRetryLimit: defaults.ServiceMirrorRetryLimit,
		logLevel:                defaults.LogLevel,
		selector:                fmt.Sprintf("%s=%s", k8s.DefaultExportedServiceSelector, "true"),
		gatewayAddresses:        "",
		gatewayPort:             0,
	}, nil
}

func buildServiceMirrorValues(opts *linkOptions) (*multicluster.Values, error) {

	if !alphaNumDashDot.MatchString(opts.controlPlaneVersion) {
		return nil, fmt.Errorf("%s is not a valid version", opts.controlPlaneVersion)
	}

	if opts.namespace == "" {
		return nil, errors.New("you need to specify a namespace")
	}

	if _, err := log.ParseLevel(opts.logLevel); err != nil {
		return nil, fmt.Errorf("--log-level must be one of: panic, fatal, error, warn, info, debug")
	}

	defaults, err := multicluster.NewLinkValues()
	if err != nil {
		return nil, err
	}

	if reg := os.Getenv(flags.EnvOverrideDockerRegistry); reg != "" {
		opts.dockerRegistry = reg
	}

	defaults.TargetClusterName = opts.clusterName
	defaults.ServiceMirrorRetryLimit = opts.serviceMirrorRetryLimit
	defaults.LogLevel = opts.logLevel
	defaults.ControllerImageVersion = opts.controlPlaneVersion
	defaults.ControllerImage = fmt.Sprintf("%s/controller", opts.dockerRegistry)

	return defaults, nil
}

func extractGatewayPort(gateway *corev1.Service) (uint32, error) {
	for _, port := range gateway.Spec.Ports {
		if port.Name == k8s.GatewayPortName {
			if gateway.Spec.Type == "NodePort" {
				return uint32(port.NodePort), nil
			}
			return uint32(port.Port), nil
		}
	}
	return 0, fmt.Errorf("gateway service %s has no gateway port named %s", gateway.Name, k8s.GatewayPortName)
}

func extractSAToken(secrets []corev1.Secret, saName string) (string, error) {
	for _, secret := range secrets {
		boundSA := secret.Annotations[saNameAnnotationKey]
		if saName == boundSA {
			token, ok := secret.Data[tokenKey]
			if !ok {
				return "", fmt.Errorf("could not find the token data in service account secret %s", secret.Name)
			}

			return string(token), nil
		}
	}

	return "", fmt.Errorf("could not find service account token secret for %s", saName)
}
