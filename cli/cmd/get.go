package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/runconduit/conduit/cli/k8s"
	"github.com/runconduit/conduit/cli/shell"

	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get [flags] RESOURCE",
	Short: "Display one or many mesh resources",
	Long: `Display one or many mesh resources.

Valid resource types include:
 * pods (aka pod, po)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		switch len(args) {
		case 1:
			resourceType := args[0]
			switch resourceType {
			case "pod", "pods", "po":
				kubeApi, err := k8s.MakeK8sAPi(shell.MakeUnixShell(), kubeconfigPath, apiAddr)
				if err != nil {
					return err
				}

				client, err := newApiClient(kubeApi)
				if err != nil {
					return err
				}
				resp, err := client.ListPods(context.Background(), &pb.Empty{})
				if err != nil {
					return err
				}

				for _, pod := range resp.GetPods() {
					fmt.Println(pod.Name)
				}

			default:
				return errors.New("invalid resource type")
			}

			return nil
		default:
			return errors.New("please specify a resource type")
		}
	},
}

func init() {
	RootCmd.AddCommand(getCmd)
	addControlPlaneNetworkingArgs(getCmd)
}
