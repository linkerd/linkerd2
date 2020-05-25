package cmd

import (
	"bufio"
	"bytes"
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
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/helm/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

const (
	defaultMulticlusterNamespace     = "linkerd-multicluster"
	helmMulticlusterDefaultChartName = "linkerd2-multicluster"
	tokenKey                         = "token"
	defaultServiceAccountName                         = "linkerd-service-mirror-remote-access-default"
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
		serviceMirror           bool
		serviceMirrorRetryLimit uint32
		logLevel                string
		gatewayNginxImage       string
		gatewayNginxVersion     string
		controlPlaneVersion     string
		dockerRegistry          string
		remoteMirrorCredentials bool
	}

	linkOptions struct {
		namespace           string
		clusterName         string
		remoteClusterDomain string
		remoteClusterServer string
		serviceAccountName  string
	}

	exportServiceOptions struct {
		gatewayNamespace string
		gatewayName      string
	}

	gatewaysOptions struct {
		gatewayNamespace string
		clusterName      string
		timeWindow       string
	}
)

func newMulticlusterInstallOptionsWithDefault() (*multiclusterInstallOptions, error) {
	defaults, err := mccharts.NewValues()
	if err != nil {
		return nil, err
	}

	return &multiclusterInstallOptions{
		gateway:                 defaults.Gateway,
		gatewayPort:             defaults.GatewayPort,
		gatewayProbeSeconds:     defaults.GatewayProbeSeconds,
		gatewayProbePort:        defaults.GatewayProbePort,
		namespace:               defaults.Namespace,
		serviceMirror:           defaults.ServiceMirror,
		serviceMirrorRetryLimit: defaults.ServiceMirrorRetryLimit,
		logLevel:                defaults.LogLevel,
		gatewayNginxImage:       defaults.GatewayNginxImage,
		gatewayNginxVersion:     defaults.GatewayNginxImageVersion,
		controlPlaneVersion:     version.Version,
		dockerRegistry:          defaultDockerRegistry,
		remoteMirrorCredentials: true,
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

func buildMulticlusterInstallValues(opts *multiclusterInstallOptions) (*multicluster.Values, error) {

	global, err := getLinkerdConfigMap()
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil, errors.New("you need Linkerd to be installed in order to install multicluster addons")
		}
		return nil, err
	}

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

	defaults, err := mccharts.NewValues()
	if err != nil {
		return nil, err
	}

	if opts.gatewayProbePort == defaults.GatewayLocalProbePort {
		return nil, fmt.Errorf("The probe port needs to be different from %d which is the local probe port", opts.gatewayProbePort)
	}

	defaults.Namespace = opts.namespace
	defaults.Gateway = opts.gateway
	defaults.GatewayPort = opts.gatewayPort
	defaults.GatewayProbeSeconds = opts.gatewayProbeSeconds
	defaults.GatewayProbePort = opts.gatewayProbePort
	defaults.ServiceMirror = opts.serviceMirror
	defaults.ServiceMirrorRetryLimit = opts.serviceMirrorRetryLimit
	defaults.LogLevel = opts.logLevel
	defaults.GatewayNginxImage = opts.gatewayNginxImage
	defaults.GatewayNginxImageVersion = opts.gatewayNginxVersion
	defaults.IdentityTrustDomain = global.Global.IdentityContext.TrustDomain
	defaults.LinkerdNamespace = controlPlaneNamespace
	defaults.ProxyOutboundPort = global.Proxy.OutboundPort.Port
	defaults.LinkerdVersion = version.Version
	defaults.ControllerImageVersion = opts.controlPlaneVersion
	defaults.ControllerImage = fmt.Sprintf("%s/controller", opts.dockerRegistry)
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

	defaults, err := mccharts.NewValues()
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
		Short:  "Outputs credential resources to that needs to be installed on the remote cluster to allow service mirror controllers to connect to it and mirror services",
		Args: cobra.NoArgs,
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
	cmd.Flags().StringVar(&opts.serviceAccountName, "service-account-name", "", "The name of the remote access service account")

	return cmd
}

func newGatewaysCommand() *cobra.Command {

	opts := gatewaysOptions{}

	cmd := &cobra.Command{
		Hidden: false,
		Use:    "gateways",
		Short:  "Display stats information about the remote gateways",
		Args:   cobra.NoArgs,
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

	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", "", "the name of the remote cluster")
	cmd.Flags().StringVar(&opts.gatewayNamespace, "gateway-namespace", "", "the namespace in which the gateway resides on the remote cluster")
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
		Hidden: false,
		Use:    "install",
		Short:  "Output Kubernetes configs to install the Linkerd multicluster add-on",
		Args:   cobra.NoArgs,
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
				{Name: "templates/service-mirror.yaml"},
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

	cmd.Flags().StringVar(&options.namespace, "namespace", options.namespace, "The namespace in which the multicluster add-on is to be installed. Must not be the control plane namespace. ")
	cmd.Flags().BoolVar(&options.gateway, "gateway", options.gateway, "If the gateway component should be installed")
	cmd.Flags().Uint32Var(&options.gatewayPort, "gateway-port", options.gatewayPort, "The port on the gateway used for all incoming traffic")
	cmd.Flags().Uint32Var(&options.gatewayProbeSeconds, "gateway-probe-seconds", options.gatewayProbeSeconds, "The interval at which the gateway will be checked for being alive in seconds")
	cmd.Flags().Uint32Var(&options.gatewayProbePort, "gateway-probe-port", options.gatewayProbePort, "The liveness check port of the gateway")
	cmd.Flags().BoolVar(&options.serviceMirror, "service-mirror", options.serviceMirror, "If the service-mirror component should be installed")
	cmd.Flags().Uint32Var(&options.serviceMirrorRetryLimit, "service-mirror-retry-limit", options.serviceMirrorRetryLimit, "The number of times a failed update from the remote cluster is allowed to be retried")
	cmd.Flags().StringVar(&options.logLevel, "log-level", options.logLevel, "Log level for the Multicluster components")
	cmd.Flags().StringVar(&options.gatewayNginxImage, "gateway-nginx-image", options.gatewayNginxImage, "The nginx image to be used")
	cmd.Flags().StringVar(&options.gatewayNginxVersion, "gateway-nginx-image-version", options.gatewayNginxVersion, "The version of nginx to be used")
	cmd.Flags().StringVarP(&options.controlPlaneVersion, "control-plane-version", "", options.controlPlaneVersion, "(Development) Tag to be used for the control plane component images")
	cmd.Flags().StringVar(&options.dockerRegistry, "registry", options.dockerRegistry, "Docker registry to pull images from")
	cmd.Flags().BoolVar(&options.remoteMirrorCredentials, "remote-mirror-credentials", options.remoteMirrorCredentials, "Whether to install the default remote access service account")

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
	opts := linkOptions{}

	cmd := &cobra.Command{
		Hidden: false,
		Use:    "link",
		Short:  "Outputs a Kubernetes secret containing the credential that can allow a service mirror component to connect to a remote cluster",
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {

			if opts.clusterName == "" {
				return errors.New("You need to specify cluster name")
			}
			
			_, err := getLinkerdConfigMap()
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

			if opts.remoteClusterServer != "" {
				cluster.Server = opts.remoteClusterServer
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
						k8s.RemoteClusterDomainAnnotation:           opts.remoteClusterDomain,
						k8s.RemoteClusterLinkerdNamespaceAnnotation: controlPlaneNamespace,
					},
				},
				Data: map[string][]byte{
					k8s.ConfigKeyName: kubeconfig,
				},
			}

			out, err := yaml.Marshal(creds)
			if err != nil {
				return err
			}
			fmt.Println(string(out))

			return nil
		},
	}

	cmd.Flags().StringVar(&opts.namespace, "namespace", defaultMulticlusterNamespace, "The namespace for the service account")
	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", "", "Cluster name")
	cmd.Flags().StringVar(&opts.remoteClusterDomain, "remote-cluster-domain", defaultClusterDomain, "Custom remote cluster domain")
	cmd.Flags().StringVar(&opts.remoteClusterServer, "cluster-server", "", "Custom remote cluster domain")
	cmd.Flags().StringVar(&opts.serviceAccountName, "service-account", defaultServiceAccountName, "The name of th service account associated with the credentials")

	return cmd
}

type exportReport struct {
	resourceKind string
	resourceName string
	exported     bool
}

func transform(bytes []byte, gatewayName, gatewayNamespace string) ([]byte, []*exportReport, error) {
	var metaType metav1.TypeMeta

	if err := yaml.Unmarshal(bytes, &metaType); err != nil {
		return nil, nil, err
	}

	if metaType.Kind == "Service" {
		var service corev1.Service
		if err := yaml.Unmarshal(bytes, &service); err != nil {
			return nil, nil, err
		}

		if service.Annotations == nil {
			service.Annotations = map[string]string{}
		}
		report := &exportReport{
			resourceKind: strings.ToLower(metaType.Kind),
			resourceName: service.Name,
		}

		if service.Labels != nil {
			if _, isMirroredResource := service.Labels[k8s.MirroredResourceLabel]; isMirroredResource {
				report.exported = false
				return bytes, []*exportReport{report}, nil
			}
		}

		service.Annotations[k8s.GatewayNameAnnotation] = gatewayName
		service.Annotations[k8s.GatewayNsAnnotation] = gatewayNamespace

		transformed, err := yaml.Marshal(service)

		if err != nil {
			return nil, nil, err
		}
		report.exported = true
		return transformed, []*exportReport{report}, nil
	}

	report := &exportReport{
		resourceKind: strings.ToLower(metaType.Kind),
		exported:     false,
	}

	return bytes, []*exportReport{report}, nil
}

func generateReport(reports []*exportReport, reportsOut io.Writer) error {
	unexportedResources := map[string]int{}

	for _, r := range reports {
		if r.exported {
			if _, err := reportsOut.Write([]byte(fmt.Sprintf("%s \"%s\" exported\n", r.resourceKind, r.resourceName))); err != nil {
				return err
			}
		} else {
			if val, ok := unexportedResources[r.resourceKind]; ok {
				unexportedResources[r.resourceKind] = val + 1
			} else {
				unexportedResources[r.resourceKind] = 1
			}
		}
	}

	if len(unexportedResources) > 0 {
		reportsOut.Write([]byte("\n"))
		reportsOut.Write([]byte("Number of skipped resources:\n"))
	}

	for res, num := range unexportedResources {
		reportsOut.Write([]byte(fmt.Sprintf("%ss: %d\n", res, num)))
	}

	return nil
}

func transformList(bytes []byte, gatewayName, gatewayNamespace string) ([]byte, []*exportReport, error) {
	var sourceList corev1.List
	if err := yaml.Unmarshal(bytes, &sourceList); err != nil {
		return nil, nil, err
	}

	reports := []*exportReport{}
	items := []runtime.RawExtension{}

	for _, item := range sourceList.Items {
		result, report, err := transform(item.Raw, gatewayName, gatewayNamespace)
		if err != nil {
			return nil, nil, err
		}

		exported, err := yaml.YAMLToJSON(result)
		if err != nil {
			return nil, nil, err
		}

		items = append(items, runtime.RawExtension{Raw: exported})
		reports = append(reports, report...)
	}

	sourceList.Items = items
	result, err := yaml.Marshal(sourceList)
	if err != nil {
		return nil, nil, err
	}
	return result, reports, nil
}

func processExportYaml(in io.Reader, out io.Writer, gatewayName, gatewayNamespace string) ([]*exportReport, error) {
	reader := yamlDecoder.NewYAMLReader(bufio.NewReaderSize(in, 4096))
	var reports []*exportReport
	// Iterate over all YAML objects in the input
	for {
		// Read a single YAML object
		bytes, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		isList, err := kindIsList(bytes)
		if err != nil {
			return nil, err
		}

		var result []byte
		var currentReports []*exportReport

		if isList {
			result, currentReports, err = transformList(bytes, gatewayName, gatewayNamespace)

		} else {
			result, currentReports, err = transform(bytes, gatewayName, gatewayNamespace)
		}

		if err != nil {
			return nil, err
		}

		reports = append(reports, currentReports...)
		out.Write(result)
		out.Write([]byte("---\n"))
	}

	return reports, nil
}

func transformExportInput(inputs []io.Reader, errWriter, outWriter io.Writer, gatewayName, gatewayNamespace string) int {
	postTransformBuf := &bytes.Buffer{}
	reportBuf := &bytes.Buffer{}
	var finalReports []*exportReport
	for _, input := range inputs {
		reports, err := processExportYaml(input, postTransformBuf, gatewayName, gatewayNamespace)
		if err != nil {
			fmt.Fprintf(errWriter, "Error transforming resources: %v\n", err)
			return 1
		}
		_, err = io.Copy(outWriter, postTransformBuf)

		if err != nil {
			fmt.Fprintf(errWriter, "Error printing YAML: %v\n", err)
			return 1
		}

		finalReports = append(finalReports, reports...)
	}

	// print error report after yaml output, for better visibility
	if err := generateReport(finalReports, reportBuf); err != nil {
		fmt.Fprintf(errWriter, "Error generating reports: %v\n", err)
		return 1
	}
	errWriter.Write([]byte("\n"))
	io.Copy(errWriter, reportBuf)
	errWriter.Write([]byte("\n"))
	return 0
}

func newExportServiceCommand() *cobra.Command {
	opts := exportServiceOptions{}

	cmd := &cobra.Command{
		Hidden: false,
		Use:    "export-service",
		Short:  "Exposes a remote service to be mirrored",
		RunE: func(cmd *cobra.Command, args []string) error {

			if len(args) < 1 {
				return fmt.Errorf("please specify a kubernetes resource file")
			}

			if opts.gatewayName == "" {
				return errors.New("The --gateway-name flag needs to be set")
			}

			if opts.gatewayNamespace == "" {
				return errors.New("The --gateway-namespace flag needs to be set")
			}

			in, err := read(args[0])
			if err != nil {
				return err
			}
			exitCode := transformExportInput(in, stderr, stdout, opts.gatewayName, opts.gatewayNamespace)
			os.Exit(exitCode)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.gatewayName, "gateway-name", "linkerd-gateway", "the name of the gateway")
	cmd.Flags().StringVar(&opts.gatewayNamespace, "gateway-namespace", defaultMulticlusterNamespace, "the namespace of the gateway")

	return cmd
}

func newCmdMulticluster() *cobra.Command {

	multiclusterCmd := &cobra.Command{

		Hidden: true,
		Use:    "multicluster [flags]",
		Aliases: []string{"mc"},
		Args:   cobra.NoArgs,
		Short:  "Manages the multicluster setup for Linkerd",
		Long: `Manages the multicluster setup for Linkerd.

This command provides subcommands to manage the multicluster support
functionality of Linkerd. You can use it to install the service mirror
components on a cluster, manage credentials and link clusters together.`,
		Example: `  # Install multicluster addons.
  linkerd --context=cluster-a cluster install | kubectl --context=cluster-a apply -f -

  # Extract mirroring cluster credentials from cluster A and install them on cluster B
  linkerd --context=cluster-a cluster link --cluster-name=remote | kubectl apply --context=cluster-b -f -

  # Export services from cluster to be available to other clusters
  kubectl get svc -o yaml | linkerd export-service - | kubectl apply -f -

  # Exporting a file from a remote URL
  linkerd export-service http://url.to/yml | kubectl apply -f -

  # Exporting all the resources inside a folder and its sub-folders.
  linkerd export-service  <folder> | kubectl apply -f -`,
	}
	
	multiclusterCmd.AddCommand(newLinkCommand())
	multiclusterCmd.AddCommand(newMulticlusterInstallCommand())
	multiclusterCmd.AddCommand(newExportServiceCommand())
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
	gatewayNameHeader      = "NAME"
	gatewayNamespaceHeader = "NAMESPACE"
	clusterNameHeader      = "CLUSTER"
	aliveHeader            = "ALIVE"
	pairedServicesHeader   = "NUM_SVC"
	latencyP50Header       = "LATENCY_P50"
	latencyP95Header       = "LATENCY_P95"
	latencyP99Header       = "LATENCY_P99"
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
			Header:    gatewayNamespaceHeader,
			Width:     9,
			Flexible:  true,
			LeftAlign: true,
		},
		table.Column{
			Header:    gatewayNameHeader,
			Width:     4,
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
		row.Namespace,
		row.Name,
		alive,
		fmt.Sprint(row.PairedServices),
		valueOrPlaceholder(fmt.Sprintf("%dms", row.LatencyMsP50)),
		valueOrPlaceholder(fmt.Sprintf("%dms", row.LatencyMsP95)),
		valueOrPlaceholder(fmt.Sprintf("%dms", row.LatencyMsP99)),
	}

}
