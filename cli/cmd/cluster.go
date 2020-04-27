package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/linkerd/linkerd2/cli/table"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/charts"
	"github.com/linkerd/linkerd2/pkg/charts/multicluster"
	mccharts "github.com/linkerd/linkerd2/pkg/charts/multicluster"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
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
	helmMulticlusterRemoteSetuprDefaultChartName = "linkerd2-multicluster-remote-setup"
	tokenKey                                     = "token"
	defaultServiceAccountName                    = "linkerd-service-mirror"
	defaultServiceAccountNs                      = "linkerd-service-mirror"
	defaultClusterName                           = "remote"
)

type (
	getCredentialsOptions struct {
		serviceAccountName      string
		serviceAccountNamespace string
		clusterName             string
		remoteClusterDomain     string
	}

	setupRemoteClusterOptions struct {
		serviceAccountName      string
		serviceAccountNamespace string
		gatewayNamespace        string
		gatewayName             string
		probePort               uint32
		incomingPort            uint32
		probePeriodSeconds      uint32
		probePath               string
		nginxImageVersion       string
		nginxImage              string
	}

	exportServiceOptions struct {
		namespace        string
		service          string
		gatewayNamespace string
		gatewayName      string
	}

	gatewaysOptions struct {
		gatewayNamespace string
		clusterName      string
		timeWindow       string
	}
)

func newSetupRemoteClusterOptionsWithDefault() (*setupRemoteClusterOptions, error) {
	defaults, err := mccharts.NewValues()
	if err != nil {
		return nil, err
	}

	return &setupRemoteClusterOptions{
		serviceAccountName:      defaults.ServiceAccountName,
		serviceAccountNamespace: defaults.ServiceAccountNamespace,
		gatewayNamespace:        defaults.GatewayNamespace,
		gatewayName:             defaults.GatewayName,
		probePort:               defaults.ProbePort,
		incomingPort:            defaults.IncomingPort,
		probePeriodSeconds:      defaults.ProbePeriodSeconds,
		probePath:               defaults.ProbePath,
		nginxImageVersion:       defaults.NginxImageVersion,
		nginxImage:              defaults.NginxImage,
	}, nil

}

func buildMulticlusterSetupValues(opts *setupRemoteClusterOptions) (*multicluster.Values, error) {

	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return nil, err
	}

	_, global, err := healthcheck.FetchLinkerdConfigMap(kubeAPI, controlPlaneNamespace)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil, errors.New("you need Linkerd to be installed in order to setup a remote cluster")
		}
		return nil, err
	}

	defaults, err := mccharts.NewValues()
	if err != nil {
		return nil, err
	}

	defaults.GatewayName = opts.gatewayName
	defaults.GatewayNamespace = opts.gatewayNamespace
	defaults.IdentityTrustDomain = global.Global.IdentityContext.TrustDomain
	defaults.IncomingPort = opts.incomingPort
	defaults.LinkerdNamespace = controlPlaneNamespace
	defaults.ProbePath = opts.probePath
	defaults.ProbePeriodSeconds = opts.probePeriodSeconds
	defaults.ProbePort = global.Proxy.OutboundPort.Port
	defaults.ProxyOutboundPort = global.Proxy.OutboundPort.Port
	defaults.ServiceAccountName = opts.serviceAccountName
	defaults.ServiceAccountNamespace = opts.serviceAccountNamespace
	defaults.NginxImageVersion = opts.nginxImageVersion
	defaults.NginxImage = opts.nginxImage
	defaults.LinkerdVersion = version.Version

	return defaults, nil
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

func newSetupRemoteCommand() *cobra.Command {
	options, err := newSetupRemoteClusterOptionsWithDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s", err)
		os.Exit(1)
	}

	cmd := &cobra.Command{
		Hidden: false,
		Use:    "setup-remote",
		Short:  "Sets up the remote cluster by creating the gateway and necessary credentials",
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {

			values, err := buildMulticlusterSetupValues(options)

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
				{Name: "templates/gateway.yaml"},
				{Name: "templates/service-mirror-rbac.yaml"},
			}

			chart := &charts.Chart{
				Name:      helmMulticlusterRemoteSetuprDefaultChartName,
				Dir:       helmMulticlusterRemoteSetuprDefaultChartName,
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

	cmd.Flags().StringVar(&options.gatewayName, "gateway-name", options.gatewayName, "the name of the gateway")
	cmd.Flags().StringVar(&options.gatewayNamespace, "gateway-namespace", options.gatewayNamespace, "the namespace in which the gateway will be installed")
	cmd.Flags().Uint32Var(&options.probePort, "probe-port", options.probePort, "the liveness check port of the gateway")
	cmd.Flags().Uint32Var(&options.incomingPort, "incoming-port", options.incomingPort, "the port on the gateway used for all incomming traffic")
	cmd.Flags().StringVar(&options.probePath, "probe-path", options.probePath, "the path that will be exercised by the liveness checks")
	cmd.Flags().Uint32Var(&options.probePeriodSeconds, "probe-period", options.probePeriodSeconds, "the interval at which the gateway will be checked for being alive in seconds")
	cmd.Flags().StringVar(&options.serviceAccountName, "service-account-name", options.serviceAccountName, "the name of the service account")
	cmd.Flags().StringVar(&options.serviceAccountNamespace, "service-account-namespace", options.serviceAccountNamespace, "the namespace in which the service account will be created")
	cmd.Flags().StringVar(&options.nginxImageVersion, "nginx-image-version", options.nginxImageVersion, "the version of nginx to be used")
	cmd.Flags().StringVar(&options.nginxImage, "nginx-image", options.nginxImage, "the nginx image to be used")

	return cmd
}

func newGetCredentialsCommand() *cobra.Command {
	opts := getCredentialsOptions{}

	cmd := &cobra.Command{
		Hidden: false,
		Use:    "get-credentials",
		Short:  "Get cluster credentials as a secret",
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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

			sa, err := k.CoreV1().ServiceAccounts(opts.serviceAccountNamespace).Get(opts.serviceAccountName, metav1.GetOptions{})
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

			secret, err := k.CoreV1().Secrets(opts.serviceAccountNamespace).Get(secretName, metav1.GetOptions{})
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
					Name: fmt.Sprintf("cluster-credentials-%s", opts.clusterName),
					Annotations: map[string]string{
						k8s.RemoteClusterNameLabel:        opts.clusterName,
						k8s.RemoteClusterDomainAnnotation: opts.remoteClusterDomain,
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

	cmd.Flags().StringVar(&opts.serviceAccountName, "service-account-name", defaultServiceAccountName, "the name of the service account")
	cmd.Flags().StringVar(&opts.serviceAccountNamespace, "service-account-namespace", defaultServiceAccountNs, "the namespace in which the service account will be created")
	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", defaultClusterName, "cluster name")
	cmd.Flags().StringVar(&opts.remoteClusterDomain, "remote-cluster-domain", defaultClusterDomain, "custom remote cluster domain")

	return cmd
}

func newExportServiceCommand() *cobra.Command {
	opts := exportServiceOptions{}

	cmd := &cobra.Command{
		Hidden: false,
		Use:    "export-service",
		Short:  "Exposes a remote service to be mirrored",
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {

			if opts.service == "" {
				return errors.New("The --service-name flag needs to be set")
			}

			if opts.namespace == "" {
				return errors.New("The --service-name flag needs to be set")
			}

			if opts.gatewayName == "" {
				return errors.New("The --gateway-name flag needs to be set")
			}

			if opts.gatewayNamespace == "" {
				return errors.New("The --gateway-namespace flag needs to be set")
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

			svc, err := k.CoreV1().Services(opts.namespace).Get(opts.service, metav1.GetOptions{})
			if err != nil {
				return err
			}

			_, hasGatewayName := svc.Annotations[k8s.GatewayNameAnnotation]
			_, hasGatewayNs := svc.Annotations[k8s.GatewayNsAnnotation]

			if hasGatewayName || hasGatewayNs {
				return fmt.Errorf("service %s/%s has already been exported", svc.Namespace, svc.Name)
			}

			svc.Annotations[k8s.GatewayNameAnnotation] = opts.gatewayName
			svc.Annotations[k8s.GatewayNsAnnotation] = opts.gatewayNamespace

			_, err = k.CoreV1().Services(svc.Namespace).Update(svc)
			if err != nil {
				return err
			}

			fmt.Println(fmt.Sprintf("Service %s/%s is now exported", svc.Namespace, svc.Name))
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.service, "service-name", "", "the name of the service to be exported")
	cmd.Flags().StringVar(&opts.namespace, "service-namespace", "", "the namespace in which the service to be exported resides")
	cmd.Flags().StringVar(&opts.gatewayName, "gateway-name", "linkerd-gateway", "the name of the gateway")
	cmd.Flags().StringVar(&opts.gatewayNamespace, "gateway-namespace", "linkerd-gateway", "the namespace of the gateway")

	return cmd
}

func newCmdCluster() *cobra.Command {

	clusterCmd := &cobra.Command{

		Hidden: true,
		Use:    "cluster [flags]",
		Args:   cobra.NoArgs,
		Short:  "Manages the multicluster setup for Linkerd",
		Long: `Manages the multicluster setup for Linkerd.

This command provides subcommands to manage the multicluster support
functionality of Linkerd. You can use it to deploy credentials to
remote clusters, extract them as well as export remote services to be
available across clusters.`,
		Example: `  # Setup remote cluster.
  linkerd --context=cluster-a cluster setup-remote | kubectl --context=cluster-a apply -f -

  # Extract mirroring cluster credentials from cluster A and install them on cluster B
  linkerd --context=cluster-a cluster get-credentials --cluster-name=remote | kubectl apply --context=cluster-b -f -

  # Export service from cluster A to be available to other clusters
  linkerd --context=cluster-a cluster export-service --service-name=backend-svc --service-namespace=default --gateway-name=linkerd-gateway --gateway-ns=default`,
	}

	clusterCmd.AddCommand(newGetCredentialsCommand())
	clusterCmd.AddCommand(newSetupRemoteCommand())
	clusterCmd.AddCommand(newExportServiceCommand())
	clusterCmd.AddCommand(newGatewaysCommand())

	return clusterCmd
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
