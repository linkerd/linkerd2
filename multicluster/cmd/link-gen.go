package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha3"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/yaml"
)

type (
	linkGenOptions struct {
		namespace                string
		clusterName              string
		apiServerAddress         string
		serviceAccountName       string
		gatewayName              string
		gatewayNamespace         string
		selector                 string
		remoteDiscoverySelector  string
		federatedServiceSelector string
		gatewayAddresses         string
		gatewayPort              uint32
		excludedAnnotations      []string
		excludedLabels           []string
		enableGateway            bool
		output                   string
	}
)

func newGenCommand() *cobra.Command {
	opts := newLinkGenOptionsWithDefault()

	cmd := &cobra.Command{
		Use:   "link-gen",
		Short: "Outputs a Link manifest and credentials for another cluster to mirror services from this one",
		Long: `Outputs a Link manifest and credentials for another cluster to mirror services from this one.

Note that the Link resource applies only in one direction. In order for two
clusters to mirror each other, a Link resource will have to be generated for
each cluster and applied to the other.`,
		Args: cobra.NoArgs,
		Example: `  # To link the west cluster to east
  linkerd --context=east multicluster link-gen --cluster-name east | kubectl --context=west apply -f -
  `,
		RunE: func(cmd *cobra.Command, args []string) error {

			if opts.clusterName == "" {
				return errors.New("you need to specify cluster name")
			}

			configMap, err := getLinkerdConfigMap(cmd.Context())
			if err != nil {
				if kerrors.IsNotFound(err) {
					return errors.New("you need Linkerd to be installed on a cluster in order to get its credentials")
				}
				return err
			}

			k, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			kubeconfig, err := getKubeconfig(cmd.Context(), k, opts)
			if err != nil {
				return err
			}

			creds, err := getCreds(kubeconfig, opts.clusterName, opts.namespace, nil, nil, opts.output)
			if err != nil {
				return err
			}

			destinationLabels := map[string]string{
				clusterNameLabel: opts.clusterName,
			}
			destinationAnnotations := map[string]string{
				trustDomainAnnotation:   configMap.IdentityTrustDomain,
				clusterDomainAnnotation: configMap.ClusterDomain,
			}
			destinationCreds, err := getCreds(kubeconfig, opts.clusterName, controlPlaneNamespace, destinationLabels, destinationAnnotations, opts.output)
			if err != nil {
				return err
			}

			link, err := getLink(cmd.Context(), k, configMap.ClusterDomain, opts)
			if err != nil {
				return err
			}

			separator := []byte("---\n")
			if opts.output == "json" {
				separator = []byte("\n")
			}
			stdout.Write(creds)
			stdout.Write(separator)
			stdout.Write(destinationCreds)
			stdout.Write(separator)
			stdout.Write(link)
			stdout.Write(separator)

			return nil
		},
	}

	cmd.Flags().StringVar(&opts.namespace, "namespace", defaultMulticlusterNamespace, "The namespace for the service account")
	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", "", "Cluster name")
	cmd.Flags().StringVar(&opts.apiServerAddress, "api-server-address", "", "The api server address of the target cluster")
	cmd.Flags().StringVar(&opts.serviceAccountName, "service-account-name", defaultServiceAccountName, "The name of the service account associated with the credentials")
	cmd.Flags().StringVar(&opts.gatewayName, "gateway-name", defaultGatewayName, "The name of the gateway service")
	cmd.Flags().StringVar(&opts.gatewayNamespace, "gateway-namespace", defaultMulticlusterNamespace, "The namespace of the gateway service")
	cmd.Flags().StringVarP(&opts.selector, "selector", "l", opts.selector, "Selector (label query) to filter which services in the target cluster to mirror")
	cmd.Flags().StringVar(&opts.remoteDiscoverySelector, "remote-discovery-selector", opts.remoteDiscoverySelector, "Selector (label query) to filter which services in the target cluster to mirror in remote discovery mode")
	cmd.Flags().StringVar(&opts.federatedServiceSelector, "federated-service-selector", opts.federatedServiceSelector, "Selector (label query) for federated service members in the target cluster")
	cmd.Flags().StringVar(&opts.gatewayAddresses, "gateway-addresses", opts.gatewayAddresses, "If specified, overwrites gateway addresses when gateway service is not type LoadBalancer (comma separated list)")
	cmd.Flags().Uint32Var(&opts.gatewayPort, "gateway-port", opts.gatewayPort, "If specified, overwrites gateway port when gateway service is not type LoadBalancer")
	cmd.Flags().StringSliceVar(&opts.excludedAnnotations, "excluded-annotations", opts.excludedAnnotations, "Annotations to exclude when mirroring services")
	cmd.Flags().StringSliceVar(&opts.excludedLabels, "excluded-labels", opts.excludedLabels, "Labels to exclude when mirroring services")
	cmd.Flags().BoolVar(&opts.enableGateway, "gateway", opts.enableGateway, "If false, allows a link to be created against a cluster that does not have a gateway service")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "yaml", "Output format. One of: json|yaml")

	pkgcmd.ConfigureNamespaceFlagCompletion(
		cmd, []string{"namespace", "gateway-namespace"},
		kubeconfigPath, impersonate, impersonateGroup, kubeContext)
	return cmd
}

func getKubeconfig(ctx context.Context, k *k8s.KubernetesAPI, opts *linkGenOptions) ([]byte, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = kubeconfigPath
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
	config, err := loader.RawConfig()
	if err != nil {
		return nil, err
	}

	if kubeContext != "" {
		config.CurrentContext = kubeContext
	}

	sa, err := k.CoreV1().ServiceAccounts(opts.namespace).Get(ctx, opts.serviceAccountName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	listOpts := metav1.ListOptions{
		FieldSelector: fmt.Sprintf("type=%s", corev1.SecretTypeServiceAccountToken),
	}
	secrets, err := k.CoreV1().Secrets(opts.namespace).List(ctx, listOpts)
	if err != nil {
		return nil, err
	}

	token, err := extractSAToken(secrets.Items, sa.Name)
	if err != nil {
		return nil, err
	}

	context, ok := config.Contexts[config.CurrentContext]
	if !ok {
		return nil, fmt.Errorf("could not extract current context from config")
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

	return clientcmd.Write(config)
}

func getCreds(kubeconfig []byte, clusterName, namespace string, labels, annotations map[string]string, output string) ([]byte, error) {
	creds := corev1.Secret{
		Type:     k8s.MirrorSecretType,
		TypeMeta: metav1.TypeMeta{Kind: "Secret", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("cluster-credentials-%s", clusterName),
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Data: map[string][]byte{
			k8s.ConfigKeyName: kubeconfig,
		},
	}

	var credsOut []byte
	var err error

	switch output {
	case "yaml":
		credsOut, err = yaml.Marshal(creds)
		if err != nil {
			return nil, err
		}
	case "json":
		credsOut, err = json.Marshal(creds)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("output format %s not supported", output)
	}

	return credsOut, nil
}

func getLink(ctx context.Context, k *k8s.KubernetesAPI, clusterDomain string, opts *linkGenOptions) ([]byte, error) {
	remoteDiscoverySelector, err := metav1.ParseToLabelSelector(opts.remoteDiscoverySelector)
	if err != nil {
		return nil, err
	}

	federatedServiceSelector, err := metav1.ParseToLabelSelector(opts.federatedServiceSelector)
	if err != nil {
		return nil, err
	}

	link := v1alpha3.Link{
		TypeMeta: metav1.TypeMeta{Kind: "Link", APIVersion: "multicluster.linkerd.io/v1alpha3"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.clusterName,
			Namespace: opts.namespace,
			Annotations: map[string]string{
				k8s.CreatedByAnnotation: k8s.CreatedByAnnotationValue(),
			},
		},
		Spec: v1alpha3.LinkSpec{
			TargetClusterName:             opts.clusterName,
			TargetClusterDomain:           clusterDomain,
			TargetClusterLinkerdNamespace: controlPlaneNamespace,
			ClusterCredentialsSecret:      fmt.Sprintf("cluster-credentials-%s", opts.clusterName),
			RemoteDiscoverySelector:       remoteDiscoverySelector,
			FederatedServiceSelector:      federatedServiceSelector,
			ExcludedAnnotations:           opts.excludedAnnotations,
			ExcludedLabels:                opts.excludedLabels,
		},
	}

	// If there is a gateway in the exporting cluster, populate Link
	// resource with gateway information
	if opts.enableGateway {
		gateway, err := k.CoreV1().Services(opts.gatewayNamespace).Get(ctx, opts.gatewayName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

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

		if opts.gatewayAddresses != "" {
			link.Spec.GatewayAddress = opts.gatewayAddresses
		} else if len(gwAddresses) > 0 {
			link.Spec.GatewayAddress = strings.Join(gwAddresses, ",")
		} else {
			return nil, fmt.Errorf("gateway %s.%s has no ingress addresses", gateway.Name, gateway.Namespace)
		}

		gatewayIdentity, ok := gateway.Annotations[k8s.GatewayIdentity]
		if !ok || gatewayIdentity == "" {
			return nil, fmt.Errorf("gateway %s.%s has no %s annotation", gateway.Name, gateway.Namespace, k8s.GatewayIdentity)
		}
		link.Spec.GatewayIdentity = gatewayIdentity

		probeSpec, err := extractProbeSpec(gateway)
		if err != nil {
			return nil, err
		}
		link.Spec.ProbeSpec = probeSpec

		gatewayPort, err := extractGatewayPort(gateway)
		if err != nil {
			return nil, err
		}

		// Override with user provided gateway port if present
		if opts.gatewayPort != 0 {
			gatewayPort = opts.gatewayPort
		}
		link.Spec.GatewayPort = fmt.Sprintf("%d", gatewayPort)

		link.Spec.Selector, err = metav1.ParseToLabelSelector(opts.selector)
		if err != nil {
			return nil, err
		}
	}

	var linkOut []byte
	switch opts.output {
	case "yaml":
		linkOut, err = yaml.Marshal(link)
		if err != nil {
			return nil, err
		}
	case "json":
		linkOut, err = json.Marshal(link)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("output format %s not supported", opts.output)
	}

	return linkOut, nil
}

func newLinkGenOptionsWithDefault() *linkGenOptions {
	return &linkGenOptions{
		namespace:                defaultMulticlusterNamespace,
		selector:                 fmt.Sprintf("%s=%s", k8s.DefaultExportedServiceSelector, "true"),
		remoteDiscoverySelector:  fmt.Sprintf("%s=%s", k8s.DefaultExportedServiceSelector, "remote-discovery"),
		federatedServiceSelector: fmt.Sprintf("%s=%s", k8s.DefaultFederatedServiceSelector, "member"),
		gatewayAddresses:         "",
		gatewayPort:              0,
		excludedAnnotations:      []string{},
		excludedLabels:           []string{},
		enableGateway:            true,
	}
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

// ExtractProbeSpec parses the ProbSpec from a gateway service's annotations.
// For now we're not including the failureThreshold and timeout fields which
// are new since edge-24.9.3, to avoid errors when attempting to apply them in
// clusters with an older Link CRD.
func extractProbeSpec(gateway *corev1.Service) (v1alpha3.ProbeSpec, error) {
	path := gateway.Annotations[k8s.GatewayProbePath]
	if path == "" {
		return v1alpha3.ProbeSpec{}, errors.New("probe path is empty")
	}

	port, err := extractPort(gateway.Spec, k8s.ProbePortName)
	if err != nil {
		return v1alpha3.ProbeSpec{}, err
	}

	// the `mirror.linkerd.io/probe-period` annotation is initialized with a
	// default value of "3", but we require a duration-formatted string. So we
	// perform the conversion, if required.
	period := gateway.Annotations[k8s.GatewayProbePeriod]
	if secs, err := strconv.ParseInt(period, 10, 64); err == nil {
		dur := time.Duration(secs) * time.Second
		period = dur.String()
	} else if _, err := time.ParseDuration(period); err != nil {
		return v1alpha3.ProbeSpec{}, fmt.Errorf("could not parse probe period: %w", err)
	}

	return v1alpha3.ProbeSpec{
		Path:   path,
		Port:   fmt.Sprintf("%d", port),
		Period: period,
	}, nil
}

func extractPort(spec corev1.ServiceSpec, portName string) (uint32, error) {
	for _, p := range spec.Ports {
		if p.Name == portName {
			if spec.Type == "NodePort" {
				return uint32(p.NodePort), nil
			}
			return uint32(p.Port), nil
		}
	}
	return 0, fmt.Errorf("could not find port with name %s", portName)
}
