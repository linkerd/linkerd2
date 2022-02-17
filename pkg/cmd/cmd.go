package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/k8s/resource"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
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

// Uninstall prints all cluster-scoped resources matching the given selector
// for the purposes of deleting them.
func Uninstall(ctx context.Context, k8sAPI *k8s.KubernetesAPI, selector string) error {
	resources, err := resource.FetchKubernetesResources(ctx, k8sAPI,
		metav1.ListOptions{LabelSelector: selector},
	)
	if err != nil {
		return err
	}

	if len(resources) == 0 {
		return errors.New("No resources found to uninstall")
	}
	for _, r := range resources {
		if err := r.RenderResource(os.Stdout); err != nil {
			return fmt.Errorf("error rendering Kubernetes resource: %w", err)
		}
	}
	return nil
}

// ConfigureNamespaceFlagCompletion sets up resource-aware completion for command
// flags that accept a namespace name
func ConfigureNamespaceFlagCompletion(
	cmd *cobra.Command,
	flagNames []string,
	kubeconfigPath string,
	impersonate string,
	impersonateGroup []string,
	kubeContext string,
) {
	for _, flagName := range flagNames {
		cmd.RegisterFlagCompletionFunc(flagName,
			func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
				k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
				if err != nil {
					return nil, cobra.ShellCompDirectiveError
				}

				cc := k8s.NewCommandCompletion(k8sAPI, "")
				results, err := cc.Complete([]string{k8s.Namespace}, toComplete)
				if err != nil {
					return nil, cobra.ShellCompDirectiveError
				}

				return results, cobra.ShellCompDirectiveDefault
			})
	}
}

// ConfigureOutputFlagCompletion sets up resource-aware completion for command
// flags that accept an output name.
func ConfigureOutputFlagCompletion(cmd *cobra.Command) {
	cmd.RegisterFlagCompletionFunc("output",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return []string{"basic", "json", "short", "table"}, cobra.ShellCompDirectiveDefault
		})
}

// ConfigureKubeContextFlagCompletion sets up resource-aware completion for command
// flags based off of a kubeconfig
func ConfigureKubeContextFlagCompletion(cmd *cobra.Command, kubeconfigPath string) {
	cmd.RegisterFlagCompletionFunc("context",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			rules := clientcmd.NewDefaultClientConfigLoadingRules()
			rules.ExplicitPath = kubeconfigPath
			loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
			config, err := loader.RawConfig()
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}

			suggestions := []string{}
			uniqContexts := map[string]struct{}{}
			for ctxName := range config.Contexts {
				if strings.HasPrefix(ctxName, toComplete) {
					if _, ok := uniqContexts[ctxName]; !ok {
						suggestions = append(suggestions, ctxName)
						uniqContexts[ctxName] = struct{}{}
					}
				}
			}

			return suggestions, cobra.ShellCompDirectiveDefault
		})
}

// GetLabelSelector creates a label selector as a string based on a label key
// whose value may be in the set provided as an argument to the function. If the
// value set is empty then the selector will match resources where the label key
// exists regardless of value.
func GetLabelSelector(labelKey string, labelValues ...string) (string, error) {
	selectionOp := selection.In
	if len(labelValues) < 1 {
		selectionOp = selection.Exists
	}

	labelRequirement, err := labels.NewRequirement(labelKey, selectionOp, labelValues)
	if err != nil {
		return "", err
	}

	selector := labels.NewSelector().Add(*labelRequirement)
	return selector.String(), nil
}

// RegistryOverride replaces the registry-portion of the provided image with the provided registry.
func RegistryOverride(image, newRegistry string) string {
	if image == "" {
		return image
	}
	registry := newRegistry
	if registry != "" && !strings.HasSuffix(registry, "/") {
		registry += "/"
	}
	imageName := image
	if strings.Contains(image, "/") {
		imageName = image[strings.LastIndex(image, "/")+1:]
	}
	return registry + imageName
}
