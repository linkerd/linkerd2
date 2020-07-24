package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/linkerd/linkerd2/cli/table"
	configPb "github.com/linkerd/linkerd2/controller/gen/config"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/charts"
	"github.com/linkerd/linkerd2/pkg/charts/multicluster"
	mccharts "github.com/linkerd/linkerd2/pkg/charts/multicluster"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	mc "github.com/linkerd/linkerd2/pkg/multicluster"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/helm/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

const (
	defaultMulticlusterNamespace         = "linkerd-multicluster"
	defaultGatewayName                   = "linkerd-gateway"
	helmMulticlusterDefaultChartName     = "linkerd2-multicluster"
	helmMulticlusterLinkDefaultChartName = "linkerd2-multicluster-link"
	tokenKey                             = "token"
	defaultServiceAccountName            = "linkerd-service-mirror-remote-access-default"
)

type (
	allowOptions struct {
		namespace          string
		serviceAccountName string
		ignoreCluster      bool
	}

	multiclusterInstallOptions struct {
		gateway                 bool
		gatewayPort             uint32
		gatewayProbeSeconds     uint32
		gatewayProbePort        uint32
		namespace               string
		gatewayNginxImage       string
		gatewayNginxVersion     string
		dockerRegistry          string
		remoteMirrorCredentials bool
	}

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
	}

	gatewaysOptions struct {
		gatewayNamespace string
		clusterName      string
		timeWindow       string
	}
)

func newMulticlusterInstallOptionsWithDefault() (*multiclusterInstallOptions, error) {
	defaults, err := mccharts.NewInstallValues()
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
		dockerRegistry:          defaultDockerRegistry,
		remoteMirrorCredentials: true,
	}, nil
}

func newLinkOptionsWithDefault() (*linkOptions, error) {
	defaults, err := mccharts.NewLinkValues()
	if err != nil {
		return nil, err
	}

	return &linkOptions{
		controlPlaneVersion:     version.Version,
		namespace:               defaults.Namespace,
		dockerRegistry:          defaultDockerRegistry,
		serviceMirrorRetryLimit: defaults.ServiceMirrorRetryLimit,
		logLevel:                defaults.LogLevel,
		selector:                k8s.DefaultExportedServiceSelector,
	}, nil
}

func getLinkerdConfigMap() (*configPb.All, error) {
	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return nil, err
	}

	_, global, err := healthcheck.FetchLinkerdConfigMap(kubeAPI, controlPlaneNamespace)
	if err != nil {
		return nil, err
	}

	return global, nil
}

func buildServiceMirrorValues(opts *linkOptions) (*multicluster.Values, error) {

	if !alphaNumDashDot.MatchString(opts.controlPlaneVersion) {
		return nil, fmt.Errorf("%s is not a valid version", opts.controlPlaneVersion)
	}

	if opts.namespace == "" {
		return nil, errors.New("you need to specify a namespace")
	}

	if opts.namespace == controlPlaneNamespace {
		return nil, errors.New("you need to setup the multicluster addons in a namespace different than the Linkerd one")
	}

	if _, err := log.ParseLevel(opts.logLevel); err != nil {
		return nil, fmt.Errorf("--log-level must be one of: panic, fatal, error, warn, info, debug")
	}

	defaults, err := mccharts.NewLinkValues()
	if err != nil {
		return nil, err
	}

	defaults.TargetClusterName = opts.clusterName
	defaults.Namespace = opts.namespace
	defaults.ServiceMirrorRetryLimit = opts.serviceMirrorRetryLimit
	defaults.LogLevel = opts.logLevel
	defaults.ControllerImageVersion = opts.controlPlaneVersion
	defaults.ControllerImage = fmt.Sprintf("%s/controller", opts.dockerRegistry)

	return defaults, nil
}

func buildMulticlusterInstallValues(opts *multiclusterInstallOptions) (*multicluster.Values, error) {

	global, err := getLinkerdConfigMap()
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

	defaults, err := mccharts.NewInstallValues()
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
	defaults.IdentityTrustDomain = global.Global.IdentityContext.TrustDomain
	defaults.LinkerdNamespace = controlPlaneNamespace
	defaults.ProxyOutboundPort = global.Proxy.OutboundPort.Port
	defaults.LinkerdVersion = version.Version
	defaults.RemoteMirrorServiceAccount = opts.remoteMirrorCredentials

	return defaults, nil
}

func buildMulticlusterAllowValues(opts *allowOptions) (*mccharts.Values, error) {

	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return nil, err
	}

	if opts.namespace == "" {
		return nil, errors.New("you need to specify a namespace")
	}

	if opts.serviceAccountName == "" {
		return nil, errors.New("you need to specify a service account name")
	}

	if opts.namespace == controlPlaneNamespace {
		return nil, errors.New("you need to setup the multicluster addons in a namespace different than the Linkerd one")
	}

	defaults, err := mccharts.NewInstallValues()
	if err != nil {
		return nil, err
	}

	defaults.Namespace = opts.namespace
	defaults.LinkerdVersion = version.Version
	defaults.Gateway = false
	defaults.ServiceMirror = false
	defaults.RemoteMirrorServiceAccount = true
	defaults.RemoteMirrorServiceAccountName = opts.serviceAccountName

	if !opts.ignoreCluster {
		acc, err := kubeAPI.CoreV1().ServiceAccounts(defaults.Namespace).Get(defaults.RemoteMirrorServiceAccountName, metav1.GetOptions{})
		if err == nil && acc != nil {
			return nil, fmt.Errorf("Service account with name %s already exists, use --ignore-cluster for force operation", defaults.RemoteMirrorServiceAccountName)
		}
		if !kerrors.IsNotFound(err) {
			return nil, err
		}
	}

	return defaults, nil
}

func newAllowCommand() *cobra.Command {
	opts := allowOptions{
		namespace:     defaultMulticlusterNamespace,
		ignoreCluster: false,
	}

	cmd := &cobra.Command{
		Hidden: false,
		Use:    "allow",
		Short:  "Outputs credential resources that allow service-mirror controllers to connect to this cluster",
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {

			values, err := buildMulticlusterAllowValues(&opts)
			if err != nil {
				return err
			}

			// Render raw values and create chart config
			rawValues, err := yaml.Marshal(values)
			if err != nil {
				return err
			}

			files := []*chartutil.BufferedFile{
				{Name: chartutil.ChartfileName},
				{Name: "templates/namespace.yaml"},
				{Name: "templates/remote-access-service-mirror-rbac.yaml"},
			}

			chart := &charts.Chart{
				Name:      helmMulticlusterDefaultChartName,
				Dir:       helmMulticlusterDefaultChartName,
				Namespace: controlPlaneNamespace,
				RawValues: rawValues,
				Files:     files,
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

	cmd.Flags().StringVar(&opts.namespace, "namespace", defaultMulticlusterNamespace, "The destination namespace for the service account.")
	cmd.Flags().BoolVar(&opts.ignoreCluster, "ignore-cluster", false, "Ignore cluster configuration")
	cmd.Flags().StringVar(&opts.serviceAccountName, "service-account-name", "", "The name of the multicluster access service account")

	return cmd
}

func newGatewaysCommand() *cobra.Command {

	opts := gatewaysOptions{}

	cmd := &cobra.Command{
		Use:   "gateways",
		Short: "Display stats information about the gateways in target clusters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			req := &pb.GatewaysRequest{
				RemoteClusterName: opts.clusterName,
				GatewayNamespace:  opts.gatewayNamespace,
				TimeWindow:        opts.timeWindow,
			}

			client := checkPublicAPIClientOrExit()
			resp, err := requestGatewaysFromAPI(client, req)
			if err != nil {
				return err
			}

			renderGateways(resp.GetOk().GatewaysTable.Rows, stdout)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", "", "the name of the target cluster")
	cmd.Flags().StringVar(&opts.gatewayNamespace, "gateway-namespace", "", "the namespace in which the gateway resides on the target cluster")
	cmd.Flags().StringVarP(&opts.timeWindow, "time-window", "t", "1m", "Time window (for example: \"15s\", \"1m\", \"10m\", \"1h\"). Needs to be at least 15s.")

	return cmd
}

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

			values, err := buildMulticlusterInstallValues(options)

			if err != nil {
				return err
			}

			// Render raw values and create chart config
			rawValues, err := yaml.Marshal(values)
			if err != nil {
				return err
			}

			files := []*chartutil.BufferedFile{
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
	cmd.Flags().StringVar(&options.dockerRegistry, "registry", options.dockerRegistry, "Docker registry to pull images from")
	cmd.Flags().BoolVar(&options.remoteMirrorCredentials, "service-mirror-credentials", options.remoteMirrorCredentials, "Whether to install the service account which can be used by service mirror components in source clusters to discover exported servivces")

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

func newLinkCommand() *cobra.Command {
	opts, err := newLinkOptionsWithDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s", err)
		os.Exit(1)
	}

	cmd := &cobra.Command{
		Use:   "link",
		Short: "Outputs a Kubernetes secret that allows a service mirror component to connect to this cluster",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {

			if opts.clusterName == "" {
				return errors.New("You need to specify cluster name")
			}

			configMap, err := getLinkerdConfigMap()
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

			sa, err := k.CoreV1().ServiceAccounts(opts.namespace).Get(opts.serviceAccountName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			var secretName string
			for _, s := range sa.Secrets {
				if strings.HasPrefix(s.Name, fmt.Sprintf("%s-token", sa.Name)) {
					secretName = s.Name
					break
				}
			}
			if secretName == "" {
				return fmt.Errorf("could not find service account token secret for %s", sa.Name)
			}

			secret, err := k.CoreV1().Secrets(opts.namespace).Get(secretName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			token, ok := secret.Data[tokenKey]
			if !ok {
				return fmt.Errorf("could not find the token data in the service account secret")
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
					Token: string(token),
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
					Annotations: map[string]string{
						k8s.RemoteClusterNameLabel:                  opts.clusterName,
						k8s.RemoteClusterDomainAnnotation:           configMap.Global.ClusterDomain,
						k8s.RemoteClusterLinkerdNamespaceAnnotation: controlPlaneNamespace,
					},
				},
				Data: map[string][]byte{
					k8s.ConfigKeyName: kubeconfig,
				},
			}

			credsOut, err := yaml.Marshal(creds)
			if err != nil {
				return err
			}

			gateway, err := k.CoreV1().Services(opts.gatewayNamespace).Get(opts.gatewayName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			gatewayAddresses := []string{}
			for _, ingress := range gateway.Status.LoadBalancer.Ingress {
				gatewayAddresses = append(gatewayAddresses, ingress.IP)
			}
			if len(gatewayAddresses) == 0 {
				return fmt.Errorf("Gateway %s.%s has no ingress addresses", gateway.Name, gateway.Namespace)
			}

			gatewayIdentity, ok := gateway.Annotations[k8s.GatewayIdentity]
			if !ok || gatewayIdentity == "" {
				return fmt.Errorf("Gatway %s.%s has no %s annotation", gateway.Name, gateway.Namespace, k8s.GatewayIdentity)
			}

			probeSpec, err := mc.ExtractProbeSpec(gateway)
			if err != nil {
				return err
			}

			gatewayPort, err := extractGatewayPort(gateway)
			if err != nil {
				return err
			}

			selector, err := metav1.ParseToLabelSelector(opts.selector)
			if err != nil {
				return err
			}

			link := mc.Link{
				Name:                          opts.clusterName,
				Namespace:                     opts.namespace,
				TargetClusterName:             opts.clusterName,
				TargetClusterDomain:           configMap.Global.ClusterDomain,
				TargetClusterLinkerdNamespace: controlPlaneNamespace,
				ClusterCredentialsSecret:      fmt.Sprintf("cluster-credentials-%s", opts.clusterName),
				GatewayAddress:                strings.Join(gatewayAddresses, ","),
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

			// Render raw values and create chart config
			rawValues, err := yaml.Marshal(values)
			if err != nil {
				return err
			}

			files := []*chartutil.BufferedFile{
				{Name: chartutil.ChartfileName},
				{Name: "templates/service-mirror.yaml"},
				{Name: "templates/gateway-mirror.yaml"},
			}

			chart := &charts.Chart{
				Name:      helmMulticlusterLinkDefaultChartName,
				Dir:       helmMulticlusterLinkDefaultChartName,
				Namespace: controlPlaneNamespace,
				RawValues: rawValues,
				Files:     files,
			}
			serviceMirrorOut, err := chart.RenderNoPartials()
			if err != nil {
				return err
			}

			stdout.Write(credsOut)
			stdout.Write([]byte("---\n"))
			stdout.Write(linkOut)
			stdout.Write([]byte("---\n"))
			stdout.Write(serviceMirrorOut.Bytes())
			stdout.Write([]byte("---\n"))

			return nil
		},
	}

	cmd.Flags().StringVar(&opts.namespace, "namespace", defaultMulticlusterNamespace, "The namespace for the service account")
	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", "", "Cluster name")
	cmd.Flags().StringVar(&opts.apiServerAddress, "api-server-address", "", "The api server address of the target cluster")
	cmd.Flags().StringVar(&opts.serviceAccountName, "service-account-name", defaultServiceAccountName, "The name of the service account associated with the credentials")
	cmd.Flags().StringVar(&opts.controlPlaneVersion, "control-plane-version", opts.controlPlaneVersion, "(Development) Tag to be used for the service mirror controller image")
	cmd.Flags().StringVar(&opts.gatewayName, "gateway-name", defaultGatewayName, "The name of the gateway service")
	cmd.Flags().StringVar(&opts.gatewayNamespace, "gateway-namespace", defaultMulticlusterNamespace, "The namespace of the gateway service")
	cmd.Flags().Uint32Var(&opts.serviceMirrorRetryLimit, "service-mirror-retry-limit", opts.serviceMirrorRetryLimit, "The number of times a failed update from the target cluster is allowed to be retried")
	cmd.Flags().StringVar(&opts.logLevel, "log-level", opts.logLevel, "Log level for the Multicluster components")
	cmd.Flags().StringVar(&opts.dockerRegistry, "registry", opts.dockerRegistry, "Docker registry to pull service mirror controller image from")
	cmd.Flags().StringVarP(&opts.selector, "selector", "l", opts.selector, "Selector (label query) to filter which services in the target cluster to mirror")

	return cmd
}

func newCmdMulticluster() *cobra.Command {

	multiclusterCmd := &cobra.Command{
		Use:     "multicluster [flags]",
		Aliases: []string{"mc"},
		Args:    cobra.NoArgs,
		Short:   "Manages the multicluster setup for Linkerd",
		Long: `Manages the multicluster setup for Linkerd.

This command provides subcommands to manage the multicluster support
functionality of Linkerd. You can use it to install the service mirror
components on a cluster, manage credentials and link clusters together.`,
		Example: `  # Install multicluster addons.
  linkerd --context=cluster-a multicluster install | kubectl --context=cluster-a apply -f -

  # Extract mirroring cluster credentials from cluster A and install them on cluster B
  linkerd --context=cluster-a multicluster link --cluster-name=target | kubectl apply --context=cluster-b -f -`,
	}

	multiclusterCmd.AddCommand(newLinkCommand())
	multiclusterCmd.AddCommand(newMulticlusterInstallCommand())
	multiclusterCmd.AddCommand(newGatewaysCommand())
	multiclusterCmd.AddCommand(newAllowCommand())
	return multiclusterCmd
}

func requestGatewaysFromAPI(client pb.ApiClient, req *pb.GatewaysRequest) (*pb.GatewaysResponse, error) {
	resp, err := client.Gateways(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("Gateways API error: %v", err)
	}
	if e := resp.GetError(); e != nil {
		return nil, fmt.Errorf("Gateways API response error: %v", e.Error)
	}
	return resp, nil
}

func renderGateways(rows []*pb.GatewaysTable_Row, w io.Writer) {
	t := buildGatewaysTable()
	t.Data = []table.Row{}
	for _, row := range rows {
		row := row // Copy to satisfy golint.
		t.Data = append(t.Data, gatewaysRowToTableRow(row))
	}
	t.Render(w)
}

var (
	clusterNameHeader    = "CLUSTER"
	aliveHeader          = "ALIVE"
	pairedServicesHeader = "NUM_SVC"
	latencyP50Header     = "LATENCY_P50"
	latencyP95Header     = "LATENCY_P95"
	latencyP99Header     = "LATENCY_P99"
)

func buildGatewaysTable() table.Table {
	columns := []table.Column{
		table.Column{
			Header:    clusterNameHeader,
			Width:     7,
			Flexible:  true,
			LeftAlign: true,
		},
		table.Column{
			Header:    aliveHeader,
			Width:     5,
			Flexible:  true,
			LeftAlign: true,
		},
		table.Column{
			Header: pairedServicesHeader,
			Width:  9,
		},
		table.Column{
			Header: latencyP50Header,
			Width:  11,
		},
		table.Column{
			Header: latencyP95Header,
			Width:  11,
		},
		table.Column{
			Header: latencyP99Header,
			Width:  11,
		},
	}
	t := table.NewTable(columns, []table.Row{})
	t.Sort = []int{0, 1} // Sort by namespace, then name.
	return t
}

func gatewaysRowToTableRow(row *pb.GatewaysTable_Row) []string {
	valueOrPlaceholder := func(value string) string {
		if row.Alive {
			return value
		}
		return "-"
	}

	alive := "False"

	if row.Alive {
		alive = "True"
	}
	return []string{
		row.ClusterName,
		alive,
		fmt.Sprint(row.PairedServices),
		valueOrPlaceholder(fmt.Sprintf("%dms", row.LatencyMsP50)),
		valueOrPlaceholder(fmt.Sprintf("%dms", row.LatencyMsP95)),
		valueOrPlaceholder(fmt.Sprintf("%dms", row.LatencyMsP99)),
	}

}

func extractGatewayPort(gateway *corev1.Service) (uint32, error) {
	for _, port := range gateway.Spec.Ports {
		if port.Name == k8s.GatewayPortName {
			return uint32(port.Port), nil
		}
	}
	return 0, fmt.Errorf("gateway service %s has no gateway port named %s", gateway.Name, k8s.GatewayPortName)
}
