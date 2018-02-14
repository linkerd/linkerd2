package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get [flags] RESOURCE",
	Short: "Display one or many mesh resources",
	Long: `Display one or many mesh resources.

Valid resource types include:
 * pods (aka pod, po)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return errors.New("please specify a resource type")
		}

		if len(args) > 1 {
			return errors.New("please specify only one resource type")
		}

		friendlyName := args[0]
		resourceType, err := k8s.CanonicalKubernetesNameFromFriendlyName(friendlyName)

		if err != nil || resourceType != k8s.KubernetesPods {
			return fmt.Errorf("invalid resource type %s, only %s are allowed as resource types", friendlyName, k8s.KubernetesPods)
		}
		client, err := newPublicAPIClient()
		if err != nil {
			return err
		}

		output, err := getPods(client)
		if err != nil {
			return err
		}

		_, err = fmt.Print(output)

		return err
	},
}

func init() {
	RootCmd.AddCommand(getCmd)
	addControlPlaneNetworkingArgs(getCmd)
}

func getPods(apiClient pb.ApiClient) (string, error) {
	resp, err := apiClient.ListPods(context.Background(), &pb.Empty{})
	if err != nil {
		return "", err
	}

	return renderPods(resp)
}

func renderPods(resp *pb.ListPodsResponse) (string, error) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 0, padding, ' ', 0)
	writePodsToBuffer(resp, w)
	w.Flush()

	out := string(buffer.Bytes())

	return out, nil
}

type podRow struct {
	Status string
	Added  bool
	PodIP  string
}

func writePodsToBuffer(resp *pb.ListPodsResponse, w *tabwriter.Writer) {
	nameHeader := "NAME"
	maxNameLength := len(nameHeader)

	pods := make(map[string]*podRow)
	for _, pod := range resp.GetPods() {
		name := pod.Name

		if len(name) > maxNameLength {
			maxNameLength = len(name)
		}

		if _, ok := pods[name]; !ok {
			pods[name] = &podRow{}
		}

		pods[name].Status = pod.Status
		pods[name].Added = pod.Added
		pods[name].PodIP = pod.PodIP
	}

	fmt.Fprintln(w, strings.Join([]string{
		nameHeader+strings.Repeat(" ", maxNameLength-len(nameHeader)),
		"STATUS",
		"ADDED",
		"PODIP\t",
	}, "\t"))

	sortedNames := sortPodsKeys(pods)
	for _, name := range sortedNames {
		fmt.Fprintf(
			w,
			"%s\t%s\t%t\t%s\t\n",
			name+strings.Repeat(" ", maxNameLength-len(name)),
			pods[name].Status,
			pods[name].Added,
			pods[name].PodIP,
		)
	}
}

func sortPodsKeys(pods map[string]*podRow) []string {
	var sortedKeys []string
	for key, _ := range pods {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)
	return sortedKeys
}
