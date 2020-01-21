package cmd

import (
	"fmt"
	"strings"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

type clusterOptions struct {
	namespace      string
	serviceAccount string
	clusterName    string
}

func newCmdCluster() *cobra.Command {

	options := clusterOptions{}

	clusterCmd := &cobra.Command{
		Use:   "cluster",
		Short: "Set up cross-cluster access",
		Args:  cobra.NoArgs,
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

			sa, err := k.CoreV1().ServiceAccounts(options.namespace).Get(options.serviceAccount, metav1.GetOptions{})
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

			secret, err := k.CoreV1().Secrets(options.namespace).Get(secretName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			token := secret.Data["token"]

			context := config.Contexts[config.CurrentContext]
			context.AuthInfo = options.serviceAccount
			config.Contexts = map[string]*api.Context{
				config.CurrentContext: context,
			}
			config.AuthInfos = map[string]*api.AuthInfo{
				options.serviceAccount: &api.AuthInfo{
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
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("cluster-credentials-%s", options.clusterName),
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

	getCredentialsCmd.Flags().StringVar(&options.serviceAccount, "service-account", "linkerd-mirror", "service account")
	getCredentialsCmd.Flags().StringVarP(&options.namespace, "namespace", "n", "linkerd", "service account namespace")
	getCredentialsCmd.Flags().StringVar(&options.clusterName, "cluster-name", "remote", "cluster name")

	clusterCmd.AddCommand(getCredentialsCmd)

	return clusterCmd
}
