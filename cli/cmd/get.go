package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
)

type getOptions struct {
	namespace     string
	allNamespaces bool
}

func newGetOptions() *getOptions {
	return &getOptions{
		namespace:     "default",
		allNamespaces: false,
	}
}

func newCmdGet() *cobra.Command {
	options := newGetOptions()

	cmd := &cobra.Command{
		Use:   "get [flags] pods",
		Short: "Display one or many mesh resources",
		Long: `Display one or many mesh resources.

Only pod resources (aka pods, po) are supported.`,
		Example: `  # get all pods
  linkerd get pods

  # get pods from namespace linkerd
  linkerd get pods --namespace linkerd`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{k8s.Pod},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return errors.New("please specify a resource type")
			}

			if len(args) > 1 {
				return errors.New("please specify only one resource type")
			}

			friendlyName := args[0]
			resourceType, err := k8s.CanonicalResourceNameFromFriendlyName(friendlyName)

			if err != nil || resourceType != k8s.Pod {
				return fmt.Errorf("invalid resource type %s, valid types: %s", friendlyName, k8s.Pod)
			}

			podNames, err := getPods(checkPublicAPIClientOrExit(), options)
			if err != nil {
				return err
			}

			if len(podNames) == 0 {
				fmt.Fprintln(os.Stderr, "No resources found.")
				os.Exit(0)
			}

			for _, podName := range podNames {
				fmt.Println(podName)
			}

			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of pods")
	cmd.PersistentFlags().BoolVarP(&options.allNamespaces, "all-namespaces", "A", options.allNamespaces, "If present, returns pods across all namespaces, ignoring the \"--namespace\" flag")
	return cmd
}

func getPods(apiClient pb.ApiClient, options *getOptions) ([]string, error) {
	req := &pb.ListPodsRequest{}
	if !options.allNamespaces {
		req.Selector = &pb.ResourceSelection{
			Resource: &pb.Resource{
				Namespace: options.namespace,
			},
		}
	}

	resp, err := apiClient.ListPods(context.Background(), req)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0)
	for _, pod := range resp.GetPods() {
		names = append(names, pod.Name)
	}

	return names, nil
}
