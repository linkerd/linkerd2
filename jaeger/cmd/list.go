package cmd

import (
	"fmt"
	"os"

	"github.com/linkerd/linkerd2/jaeger/pkg/labels"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
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
		Short: "Lists which pods have tracing enabled",
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

			var tracingEnabled, tracingNotEnabled []v1.Pod

			for _, pod := range pods.Items {
				pod := pod
				if pkgK8s.IsMeshed(&pod, controlPlaneNamespace) {
					if labels.IsTracingEnabled(&pod) {
						tracingEnabled = append(tracingEnabled, pod)
					} else {
						tracingNotEnabled = append(tracingNotEnabled, pod)
					}
				}
			}

			if len(tracingEnabled) > 0 {
				fmt.Println("Pods with tracing enabled:")
				for _, pod := range tracingEnabled {
					fmt.Printf("\t* %s/%s\n", pod.Namespace, pod.Name)
				}
			}

			if len(tracingNotEnabled) > 0 {
				fmt.Println("Pods missing tracing configuration (restart these pods to enable tracing):")
				for _, pod := range tracingNotEnabled {
					fmt.Printf("\t* %s/%s\n", pod.Namespace, pod.Name)
				}
			}

			if len(tracingEnabled)+len(tracingNotEnabled) == 0 {
				fmt.Println("No meshed pods found")
			}

			return nil
		},
	}
	cmd.Flags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "The namespace to list pods in")
	cmd.Flags().BoolVarP(&options.allNamespaces, "all-namespaces", "A", options.allNamespaces, "If present, list pods across all namespaces")

	return cmd
}
