package cmd

import (
	"fmt"
	"os"

	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	vizLabels "github.com/linkerd/linkerd2/viz/pkg/labels"
	"github.com/spf13/cobra"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type listOptions struct {
	namespace     string
	allNamespaces bool
}

func newCmdList() *cobra.Command {
	var options listOptions

	cmd := &cobra.Command{
		Use:   "list [flags]",
		Short: "Lists which pods can be tapped",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			if options.namespace == "" {
				options.namespace = pkgcmd.GetDefaultNamespace(kubeconfigPath, kubeContext)
			}
			if options.allNamespaces {
				options.namespace = v1.NamespaceAll
			}

			pods, err := k8sAPI.CoreV1().Pods(options.namespace).List(cmd.Context(), metav1.ListOptions{})
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			var tapEnabled, tapDisabled, tapNotEnabled []v1.Pod

			for _, pod := range pods.Items {
				pod := pod
				if pkgK8s.IsMeshed(&pod, controlPlaneNamespace) {
					if vizLabels.IsTapDisabled(pod) {
						tapDisabled = append(tapDisabled, pod)
					} else if !vizLabels.IsTapEnabled(&pod) {
						tapNotEnabled = append(tapNotEnabled, pod)
					} else {
						tapEnabled = append(tapEnabled, pod)
					}
				}
			}

			if len(tapEnabled) > 0 {
				fmt.Println("Pods with tap enabled:")
				for _, pod := range tapEnabled {
					fmt.Printf("\t* %s/%s\n", pod.Namespace, pod.Name)
				}
			}

			if len(tapDisabled) > 0 {
				fmt.Println("Pods with tap disabled:")
				for _, pod := range tapDisabled {
					fmt.Printf("\t* %s/%s\n", pod.Namespace, pod.Name)
				}
			}

			if len(tapNotEnabled) > 0 {
				fmt.Println("Pods missing tap configuration (restart these pods to enable tap):")
				for _, pod := range tapNotEnabled {
					fmt.Printf("\t* %s/%s\n", pod.Namespace, pod.Name)
				}
			}

			if len(tapEnabled)+len(tapDisabled)+len(tapNotEnabled) == 0 {
				fmt.Println("No meshed pods found")
			}

			return nil
		},
	}
	cmd.Flags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "The namespace to list pods in")
	cmd.Flags().BoolVarP(&options.allNamespaces, "all-namespaces", "A", options.allNamespaces, "If present, list pods across all namespaces")

	return cmd
}
