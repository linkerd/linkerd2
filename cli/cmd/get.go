package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
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
  conduit get pods

  # get pods from namespace conduit
  conduit get pods --namespace conduit`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{k8s.Pods},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return errors.New("please specify a resource type")
			}

			if len(args) > 1 {
				return errors.New("please specify only one resource type")
			}

			friendlyName := args[0]
			resourceType, err := k8s.CanonicalResourceNameFromFriendlyName(friendlyName)

			if err != nil || resourceType != k8s.Pods {
				return fmt.Errorf("invalid resource type %s, only %s are allowed as resource types", friendlyName, k8s.Pods)
			}
			client, err := newPublicAPIClient()
			if err != nil {
				return err
			}

			podNames, err := getPods(client, options)
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
	cmd.PersistentFlags().BoolVar(&options.allNamespaces, "all-namespaces", options.allNamespaces, "If present, returns pods across all namespaces, ignoring the \"--namespace\" flag")
	return cmd
}

func getPods(apiClient pb.ApiClient, options *getOptions) ([]string, error) {
	req := &pb.ListPodsRequest{}
	if !options.allNamespaces {
		req.Namespace = options.namespace
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
