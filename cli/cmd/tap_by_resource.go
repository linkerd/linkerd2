package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/runconduit/conduit/controller/api/util"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/spf13/cobra"
)

var tapByResourceCmd = &cobra.Command{
	Use:   "tapByResource [flags] (RESOURCE)",
	Short: "Listen to a traffic stream",
	Long: `Listen to a traffic stream.

  The RESOURCE argument specifies the target resource(s) to tap:
  (TYPE [NAME] | TYPE/NAME)

  Examples:
  * deploy
  * deploy/my-deploy
  * deploy my-deploy
  * ns/my-ns

  Valid resource types include:

  * deployments
  * namespaces
  * pods
  * replicationcontrollers
  * services (only supported as a "--to" resource)`,
	Example: `  # tap the web deployment in the default namespace
  conduit tapByResource deploy/web

  # tap the web-dlbvj pod in the default namespace
  conduit tapByResource pod/web-dlbvj

  # tap the test namespace, filter by request to prod namespace
  conduit tapByResource ns/test --to ns/prod`,
	Args:      cobra.RangeArgs(1, 2),
	ValidArgs: util.ValidTargets,
	RunE: func(cmd *cobra.Command, args []string) error {
		req, err := buildTapByResourceRequest(
			args, namespace,
			toResource, toNamespace,
			maxRps,
			scheme, method, authority, path,
		)
		if err != nil {
			return err
		}

		client, err := newPublicAPIClient()
		if err != nil {
			return err
		}

		return requestTapByResourceFromAPI(os.Stdout, client, req)
	},
}

func init() {
	RootCmd.AddCommand(tapByResourceCmd)
	tapByResourceCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "default",
		"Namespace of the specified resource")
	tapByResourceCmd.PersistentFlags().StringVar(&toResource, "to", "",
		"Display requests to this resource")
	tapByResourceCmd.PersistentFlags().StringVar(&toNamespace, "to-namespace", "",
		"Sets the namespace used to lookup the \"--to\" resource; by default the current \"--namespace\" is used")
	tapByResourceCmd.PersistentFlags().Float32Var(&maxRps, "max-rps", 1.0,
		"Maximum requests per second to tap.")
	tapByResourceCmd.PersistentFlags().StringVar(&scheme, "scheme", "",
		"Display requests with this scheme")
	tapByResourceCmd.PersistentFlags().StringVar(&method, "method", "",
		"Display requests with this HTTP method")
	tapByResourceCmd.PersistentFlags().StringVar(&authority, "authority", "",
		"Display requests with this :authority")
	tapByResourceCmd.PersistentFlags().StringVar(&path, "path", "",
		"Display requests with paths that start with this prefix")
}

func buildTapByResourceRequest(
	resource []string, namespace string,
	toResource, toNamespace string,
	maxRps float32,
	scheme, method, authority, path string,
) (*pb.TapByResourceRequest, error) {

	target, err := util.BuildResource(namespace, resource...)
	if err != nil {
		return nil, fmt.Errorf("target resource invalid: %s", err)
	}
	if !contains(util.ValidTargets, target.Type) {
		return nil, fmt.Errorf("unsupported resource type [%s]", target.Type)
	}

	matches := []*pb.TapByResourceRequest_Match{}

	if toResource != "" {
		destination, err := util.BuildResource(toNamespace, toResource)
		if err != nil {
			return nil, fmt.Errorf("destination resource invalid: %s", err)
		}
		if !contains(util.ValidDestinations, destination.Type) {
			return nil, fmt.Errorf("unsupported resource type [%s]", target.Type)
		}

		match := pb.TapByResourceRequest_Match{
			Match: &pb.TapByResourceRequest_Match_Destinations{
				Destinations: &pb.ResourceSelection{
					Resource: &destination,
				},
			},
		}
		matches = append(matches, &match)
	}

	if scheme != "" {
		match := buildMatchHTTP(&pb.TapByResourceRequest_Match_Http{
			Match: &pb.TapByResourceRequest_Match_Http_Scheme{Scheme: scheme},
		})
		matches = append(matches, &match)
	}
	if method != "" {
		match := buildMatchHTTP(&pb.TapByResourceRequest_Match_Http{
			Match: &pb.TapByResourceRequest_Match_Http_Method{Method: method},
		})
		matches = append(matches, &match)
	}
	if authority != "" {
		match := buildMatchHTTP(&pb.TapByResourceRequest_Match_Http{
			Match: &pb.TapByResourceRequest_Match_Http_Authority{Authority: authority},
		})
		matches = append(matches, &match)
	}
	if path != "" {
		match := buildMatchHTTP(&pb.TapByResourceRequest_Match_Http{
			Match: &pb.TapByResourceRequest_Match_Http_Path{Path: path},
		})
		matches = append(matches, &match)
	}

	return &pb.TapByResourceRequest{
		Target: &pb.ResourceSelection{
			Resource: &target,
		},
		MaxRps: maxRps,
		Match: &pb.TapByResourceRequest_Match{
			Match: &pb.TapByResourceRequest_Match_All{
				All: &pb.TapByResourceRequest_Match_Seq{
					Matches: matches,
				},
			},
		},
	}, nil
}

func contains(list []string, s string) bool {
	for _, elem := range list {
		if s == elem {
			return true
		}
	}
	return false
}

func buildMatchHTTP(match *pb.TapByResourceRequest_Match_Http) pb.TapByResourceRequest_Match {
	return pb.TapByResourceRequest_Match{
		Match: &pb.TapByResourceRequest_Match_Http_{
			Http: match,
		},
	}
}

func requestTapByResourceFromAPI(w io.Writer, client pb.ApiClient, req *pb.TapByResourceRequest) error {
	rsp, err := client.TapByResource(context.Background(), req)
	if err != nil {
		return err
	}

	return renderTap(w, rsp)
}
