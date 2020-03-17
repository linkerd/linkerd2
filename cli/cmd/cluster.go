package cmd

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/rbac/v1"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

const (
	tokenKey                  = "token"
	defaultServiceAccountName = "linkerd-service-mirror"
	defaultServiceAccountNs   = "default"
	defaultClusterName        = "remote"
)

type (
	getCredentialsOptions struct {
		namespace           string
		serviceAccount      string
		clusterName         string
		remoteClusterDomain string
	}

	createOptions struct {
		namespace      string
		serviceAccount string
	}

	exportServiceOptions struct {
		namespace        string
		service          string
		gatewayNamespace string
		gatewayName      string
	}
)

func newCmdCluster() *cobra.Command {

	getOpts := getCredentialsOptions{}
	createOpts := createOptions{}
	exportOpts := exportServiceOptions{}
	clusterCmd := &cobra.Command{
		Hidden: true,
		Use:    "cluster manages the multicluster  setup for Linkerd",
		Short:  "cluster manages the multicluster  setup for Linkerd",
		Args:   cobra.NoArgs,
	}

	createCredentialsCommand := &cobra.Command{
		Hidden: true,
		Use:    "create-credentials",
		Short:  "Create the necessary credentials for service mirroring",
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {

			labels := map[string]string{
				k8s.ControllerComponentLabel: k8s.ServiceMirrorLabel,
				k8s.ControllerNSLabel:        controlPlaneNamespace,
			}

			clusterRole := v1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: createOpts.serviceAccount, Namespace: createOpts.namespace, Labels: labels},
				TypeMeta:   metav1.TypeMeta{Kind: "ClusterRole", APIVersion: "rbac.authorization.k8s.io/v1"},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"services"},
						Verbs:     []string{"list", "get", "watch"},
					},
				},
			}

			svcAccount := corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: createOpts.serviceAccount, Namespace: createOpts.namespace, Labels: labels},
				TypeMeta:   metav1.TypeMeta{Kind: v1.ServiceAccountKind, APIVersion: "v1"},
			}

			clusterRoleBinding := v1.ClusterRoleBinding{
				TypeMeta:   metav1.TypeMeta{Kind: "ClusterRoleBinding", APIVersion: "rbac.authorization.k8s.io/v1"},
				ObjectMeta: metav1.ObjectMeta{Name: createOpts.serviceAccount, Namespace: createOpts.namespace, Labels: labels},

				Subjects: []v1.Subject{
					v1.Subject{Kind: v1.ServiceAccountKind, Name: createOpts.serviceAccount, Namespace: createOpts.namespace},
				},
				RoleRef: rbacv1.RoleRef{Kind: "ClusterRole", APIGroup: "rbac.authorization.k8s.io", Name: createOpts.serviceAccount},
			}

			crOut, err := yaml.Marshal(clusterRole)
			if err != nil {
				return err
			}

			saOut, err := yaml.Marshal(svcAccount)
			if err != nil {
				return err
			}
			crbOut, err := yaml.Marshal(clusterRoleBinding)
			if err != nil {
				return err
			}
			fmt.Println(fmt.Sprintf("---\n%s---\n%s---\n%s", crOut, saOut, crbOut))
			return nil
		},
	}

	getCredentialsCmd := &cobra.Command{
		Hidden: true,
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

			sa, err := k.CoreV1().ServiceAccounts(getOpts.namespace).Get(getOpts.serviceAccount, metav1.GetOptions{})
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

			secret, err := k.CoreV1().Secrets(getOpts.namespace).Get(secretName, metav1.GetOptions{})
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

			context.AuthInfo = getOpts.serviceAccount
			config.Contexts = map[string]*api.Context{
				config.CurrentContext: context,
			}
			config.AuthInfos = map[string]*api.AuthInfo{
				getOpts.serviceAccount: {
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
					Name: fmt.Sprintf("cluster-credentials-%s", getOpts.clusterName),
					Annotations: map[string]string{
						k8s.RemoteClusterNameLabel:        getOpts.clusterName,
						k8s.RemoteClusterDomainAnnotation: getOpts.remoteClusterDomain,
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

	exportServiceCmd := &cobra.Command{
		Hidden: true,
		Use:    "export-service",
		Short:  "Exposes a remote service to be mirrored",
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

			svc, err := k.CoreV1().Services(exportOpts.namespace).Get(exportOpts.service, metav1.GetOptions{})
			if err != nil {
				return err
			}

			_, hasGatewayName := svc.Annotations[k8s.GatewayNameAnnotation]
			_, hasGatewayNs := svc.Annotations[k8s.GatewayNsAnnotation]

			if hasGatewayName || hasGatewayNs {
				return fmt.Errorf("service %s/%s has already been exported", svc.Namespace, svc.Name)
			}

			svc.Annotations[k8s.GatewayNameAnnotation] = exportOpts.gatewayName
			svc.Annotations[k8s.GatewayNsAnnotation] = exportOpts.gatewayNamespace

			_, err = k.CoreV1().Services(svc.Namespace).Update(svc)
			if err != nil {
				return err
			}

			fmt.Println(fmt.Sprintf("Service %s/%s is now exported", svc.Namespace, svc.Name))
			return nil
		},
	}
	getCredentialsCmd.Flags().StringVar(&getOpts.serviceAccount, "service-account-name", defaultServiceAccountName, "the name of the service account")
	getCredentialsCmd.Flags().StringVar(&getOpts.namespace, "service-account-namespace", defaultServiceAccountNs, "the namespace in which the service account will be created")
	getCredentialsCmd.Flags().StringVar(&getOpts.clusterName, "cluster-name", defaultClusterName, "cluster name")
	getCredentialsCmd.Flags().StringVar(&getOpts.remoteClusterDomain, "remote-cluster-domain", defaultClusterDomain, "custom remote cluster domain")

	createCredentialsCommand.Flags().StringVar(&createOpts.serviceAccount, "service-account-name", defaultServiceAccountName, "the name of the service account used")
	createCredentialsCommand.Flags().StringVar(&createOpts.namespace, "service-account-namespace", defaultServiceAccountNs, "the namespace in which the service account can be found")

	exportServiceCmd.Flags().StringVar(&exportOpts.service, "service-name", "", "the name of the service to be exported")
	exportServiceCmd.Flags().StringVar(&exportOpts.namespace, "service-namespace", "", "the namespace in which the service to be exported resides")
	exportServiceCmd.Flags().StringVar(&exportOpts.gatewayName, "gateway-name", "", "the name of the gateway")
	exportServiceCmd.Flags().StringVar(&exportOpts.gatewayNamespace, "gateway-ns", "", "the ns of the gateway")

	clusterCmd.AddCommand(getCredentialsCmd)
	clusterCmd.AddCommand(createCredentialsCommand)
	clusterCmd.AddCommand(exportServiceCmd)

	return clusterCmd
}
