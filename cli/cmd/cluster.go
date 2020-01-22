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

type getCredentialsOptions struct {
	namespace      string
	serviceAccount string
	clusterName    string
}

type createOptions struct {
	serviceAccount string
}

func newCmdCluster() *cobra.Command {

	getOpts := getCredentialsOptions{}
	createOpts := createOptions{}

	clusterCmd := &cobra.Command{
		Use:   "cluster",
		Short: "Set up cross-cluster access",
		Args:  cobra.NoArgs,
	}

	createCredentalsCommand := &cobra.Command{
		Use:   "create-credentials",
		Short: "Create the necessary credentials for service mirroring",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {

			labels := map[string]string{
				k8s.ControllerComponentLabel: "mirror",
				k8s.ControllerNSLabel:        controlPlaneNamespace,
			}

			clusterRole := v1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: createOpts.serviceAccount, Namespace: controlPlaneNamespace, Labels: labels},
				TypeMeta:   metav1.TypeMeta{Kind: "ClusterRole", APIVersion: "rbac.authorization.k8s.io/v1"},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"services"},
						Verbs:     []string{"list"},
					},
				},
			}

			svcAccount := corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: createOpts.serviceAccount, Namespace: controlPlaneNamespace, Labels: labels},
				TypeMeta:   metav1.TypeMeta{Kind: "ServiceAccount", APIVersion: "v1"},
			}

			clusterRoleBinding := v1.ClusterRoleBinding{
				TypeMeta:   metav1.TypeMeta{Kind: "ClusterRoleBinding", APIVersion: "rbac.authorization.k8s.io/v1"},
				ObjectMeta: metav1.ObjectMeta{Name: createOpts.serviceAccount, Namespace: controlPlaneNamespace, Labels: labels},

				Subjects: []v1.Subject{
					v1.Subject{Kind: v1.ServiceAccountKind, Name: createOpts.serviceAccount, Namespace: controlPlaneNamespace},
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
		Use:   "get-credentials",
		Short: "Get cluster credentials as a secret",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {

			rules := clientcmd.NewDefaultClientConfigLoadingRules()
			rules.ExplicitPath = kubeconfigPath
			overrides := &clientcmd.ConfigOverrides{CurrentContext: kubeContext}
			loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)

			config, err := loader.RawConfig()
			if err != nil {
				return err
			}

			k, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, 0)
			if err != nil {
				return err
			}

			sa, err := k.CoreV1().ServiceAccounts(getOpts.namespace).Get(getOpts.serviceAccount, metav1.GetOptions{})
			if err != nil {
				return nil
			}

			var secretName string
			for _, s := range sa.Secrets {
				if strings.HasPrefix(s.Name, fmt.Sprintf("%s-token", sa.Name)) {
					secretName = s.Name
					break
				}
			}
			if secretName == "" {
				return fmt.Errorf("Could not find service account token secret for %s", sa.Name)
			}

			secret, err := k.CoreV1().Secrets(getOpts.namespace).Get(secretName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			token := secret.Data["token"]

			context := config.Contexts[config.CurrentContext]
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
				TypeMeta: metav1.TypeMeta{Kind: "Secret", APIVersion: "v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("cluster-credentials-%s", getOpts.clusterName),
					Namespace: controlPlaneNamespace,
				},
				Data: map[string][]byte{
					"kubeconfig": kubeconfig,
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

	getCredentialsCmd.Flags().StringVar(&getOpts.serviceAccount, "service-account", "linkerd-mirror", "service account")
	getCredentialsCmd.Flags().StringVarP(&getOpts.namespace, "namespace", "n", "linkerd", "service account namespace")
	getCredentialsCmd.Flags().StringVar(&getOpts.clusterName, "cluster-name", "remote", "cluster name")

	createCredentalsCommand.Flags().StringVar(&createOpts.serviceAccount, "service-account", "linkerd-mirror", "service account")

	clusterCmd.AddCommand(getCredentialsCmd)
	clusterCmd.AddCommand(createCredentalsCommand)

	return clusterCmd
}
