package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/linkerd/linkerd2/controller/api/util"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type tapOptions struct {
	namespace   string
	toResource  string
	toNamespace string
	maxRps      float32
	scheme      string
	method      string
	authority   string
	path        string
	output      string
}

func newTapOptions() *tapOptions {
	return &tapOptions{
		namespace:   "default",
		toResource:  "",
		toNamespace: "",
		maxRps:      100.0,
		scheme:      "",
		method:      "",
		authority:   "",
		path:        "",
		output:      "",
	}
}

func newCmdTap() *cobra.Command {
	options := newTapOptions()

	cmd := &cobra.Command{
		Use:   "tap [flags] (RESOURCE)",
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
  * services (only supported as a --to resource)
  * jobs (only supported as a --to resource)`,
		Example: `  # tap the web deployment in the default namespace
  linkerd tap deploy/web

  # tap the web-dlbvj pod in the default namespace
  linkerd tap pod/web-dlbvj

  # tap the test namespace, filter by request to prod namespace
  linkerd tap ns/test --to ns/prod`,
		Args:      cobra.RangeArgs(1, 2),
		ValidArgs: util.ValidTargets,
		RunE: func(cmd *cobra.Command, args []string) error {
			requestParams := util.TapRequestParams{
				Resource:    strings.Join(args, "/"),
				Namespace:   options.namespace,
				ToResource:  options.toResource,
				ToNamespace: options.toNamespace,
				MaxRps:      options.maxRps,
				Scheme:      options.scheme,
				Method:      options.method,
				Authority:   options.authority,
				Path:        options.path,
			}

			req, err := util.BuildTapByResourceRequest(requestParams)
			if err != nil {
				return err
			}

			wide := false
			switch options.output {
			// TODO: support more output formats?
			case "":
				// default output format.
			case "wide":
				wide = true
			default:
				return fmt.Errorf("output format \"%s\" not recognized", options.output)
			}

			return requestTapByResourceFromAPI(os.Stdout, validatedPublicAPIClient(time.Time{}), req, wide)
		},
	}

	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace,
		"Namespace of the specified resource")
	cmd.PersistentFlags().StringVar(&options.toResource, "to", options.toResource,
		"Display requests to this resource")
	cmd.PersistentFlags().StringVar(&options.toNamespace, "to-namespace", options.toNamespace,
		"Sets the namespace used to lookup the \"--to\" resource; by default the current \"--namespace\" is used")
	cmd.PersistentFlags().Float32Var(&options.maxRps, "max-rps", options.maxRps,
		"Maximum requests per second to tap.")
	cmd.PersistentFlags().StringVar(&options.scheme, "scheme", options.scheme,
		"Display requests with this scheme")
	cmd.PersistentFlags().StringVar(&options.method, "method", options.method,
		"Display requests with this HTTP method")
	cmd.PersistentFlags().StringVar(&options.authority, "authority", options.authority,
		"Display requests with this :authority")
	cmd.PersistentFlags().StringVar(&options.path, "path", options.path,
		"Display requests with paths that start with this prefix")
	cmd.PersistentFlags().StringVarP(&options.output, "output", "o", options.output,
		"Output format. One of: wide")

	return cmd
}

func requestTapByResourceFromAPI(w io.Writer, client pb.ApiClient, req *pb.TapByResourceRequest, wide bool) error {
	var resource string
	if wide {
		resource = req.Target.Resource.GetType()
	}

	rsp, err := client.TapByResource(context.Background(), req)
	if err != nil {
		return err
	}
	return renderTap(w, rsp, resource)
}

func renderTap(w io.Writer, tapClient pb.Api_TapByResourceClient, resource string) error {
	tableWriter := tabwriter.NewWriter(w, 0, 0, 0, ' ', tabwriter.AlignRight)
	err := writeTapEventsToBuffer(tapClient, tableWriter, resource)
	if err != nil {
		return err
	}
	tableWriter.Flush()

	return nil
}

func writeTapEventsToBuffer(tapClient pb.Api_TapByResourceClient, w *tabwriter.Writer, resource string) error {
	for {
		log.Debug("Waiting for data...")
		event, err := tapClient.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			break
		}
		_, err = fmt.Fprintln(w, util.RenderTapEvent(event, resource))
		if err != nil {
			return err
		}
	}

	return nil
}
