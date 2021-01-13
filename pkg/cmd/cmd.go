package cmd

import (
	"github.com/prometheus/common/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
)

// GetDefaultNamespace fetches the default namespace
// used in the current KubeConfig context
func GetDefaultNamespace(kubeconfigPath, kubeContext string) string {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()

	if kubeconfigPath != "" {
		rules.ExplicitPath = kubeconfigPath
	}

	overrides := &clientcmd.ConfigOverrides{CurrentContext: kubeContext}
	kubeCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)
	ns, _, err := kubeCfg.Namespace()

	if err != nil {
		log.Warnf(`could not set namespace from kubectl context, using 'default' namespace: %s
		 ensure the KUBECONFIG path %s is valid`, err, kubeconfigPath)
		return corev1.NamespaceDefault
	}

	return ns
}
