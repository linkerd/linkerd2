package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/runconduit/conduit/controller/api/util"
	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/addr"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
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
}

func newTapOptions() *tapOptions {
	return &tapOptions{
		namespace:   "default",
		toResource:  "",
		toNamespace: "",
		maxRps:      1.0,
		scheme:      "",
		method:      "",
		authority:   "",
		path:        "",
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
  * services (only supported as a "--to" resource)`,
		Example: `  # tap the web deployment in the default namespace
  conduit tap deploy/web

  # tap the web-dlbvj pod in the default namespace
  conduit tap pod/web-dlbvj

  # tap the test namespace, filter by request to prod namespace
  conduit tap ns/test --to ns/prod`,
		Args:      cobra.RangeArgs(1, 2),
		ValidArgs: util.ValidTargets,
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildTapByResourceRequest(args, options)
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

	return cmd
}

func buildTapByResourceRequest(
	resource []string,
	options *tapOptions,
) (*pb.TapByResourceRequest, error) {

	target, err := util.BuildResource(options.namespace, resource...)
	if err != nil {
		return nil, fmt.Errorf("target resource invalid: %s", err)
	}
	if !contains(util.ValidTargets, target.Type) {
		return nil, fmt.Errorf("unsupported resource type [%s]", target.Type)
	}

	matches := []*pb.TapByResourceRequest_Match{}

	if options.toResource != "" {
		destination, err := util.BuildResource(options.toNamespace, options.toResource)
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

	if options.scheme != "" {
		match := buildMatchHTTP(&pb.TapByResourceRequest_Match_Http{
			Match: &pb.TapByResourceRequest_Match_Http_Scheme{Scheme: options.scheme},
		})
		matches = append(matches, &match)
	}
	if options.method != "" {
		match := buildMatchHTTP(&pb.TapByResourceRequest_Match_Http{
			Match: &pb.TapByResourceRequest_Match_Http_Method{Method: options.method},
		})
		matches = append(matches, &match)
	}
	if options.authority != "" {
		match := buildMatchHTTP(&pb.TapByResourceRequest_Match_Http{
			Match: &pb.TapByResourceRequest_Match_Http_Authority{Authority: options.authority},
		})
		matches = append(matches, &match)
	}
	if options.path != "" {
		match := buildMatchHTTP(&pb.TapByResourceRequest_Match_Http{
			Match: &pb.TapByResourceRequest_Match_Http_Path{Path: options.path},
		})
		matches = append(matches, &match)
	}

	return &pb.TapByResourceRequest{
		Target: &pb.ResourceSelection{
			Resource: &target,
		},
		MaxRps: options.maxRps,
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

func renderTap(w io.Writer, tapClient pb.Api_TapByResourceClient) error {
	tableWriter := tabwriter.NewWriter(w, 0, 0, 0, ' ', tabwriter.AlignRight)
	err := writeTapEventsToBuffer(tapClient, tableWriter)
	if err != nil {
		return err
	}
	tableWriter.Flush()

	return nil
}

func writeTapEventsToBuffer(tapClient pb.Api_TapByResourceClient, w *tabwriter.Writer) error {
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
		_, err = fmt.Fprintln(w, renderTapEvent(event))
		if err != nil {
			return err
		}
	}

	return nil
}

func renderTapEvent(event *common.TapEvent) string {
	dst := addr.AddressToString(event.GetDestination())
	dstLabels := event.GetDestinationMeta().GetLabels()
	dstPod := dstLabels["pod"]
	isSecured := "no"

	if dstLabels["tls"] == "true" {
		isSecured = "yes"
	}

	if dstPod != "" {
		dst = dstPod
	}

	flow := fmt.Sprintf("src=%s dst=%s",
		addr.AddressToString(event.GetSource()),
		dst,
	)

	switch ev := event.GetHttp().GetEvent().(type) {
	case *common.TapEvent_Http_RequestInit_:
		return fmt.Sprintf("req id=%d:%d %s :method=%s :authority=%s :path=%s secured=%s",
			ev.RequestInit.GetId().GetBase(),
			ev.RequestInit.GetId().GetStream(),
			flow,
			ev.RequestInit.GetMethod().GetRegistered().String(),
			ev.RequestInit.GetAuthority(),
			ev.RequestInit.GetPath(),
			isSecured,
		)

	case *common.TapEvent_Http_ResponseInit_:
		return fmt.Sprintf("rsp id=%d:%d %s :status=%d latency=%dµs secured=%s",
			ev.ResponseInit.GetId().GetBase(),
			ev.ResponseInit.GetId().GetStream(),
			flow,
			ev.ResponseInit.GetHttpStatus(),
			ev.ResponseInit.GetSinceRequestInit().GetNanos()/1000,
			isSecured,
		)

	case *common.TapEvent_Http_ResponseEnd_:
		switch eos := ev.ResponseEnd.GetEos().GetEnd().(type) {
		case *common.Eos_GrpcStatusCode:
			return fmt.Sprintf("end id=%d:%d %s grpc-status=%s duration=%dµs response-length=%dB secured=%s",
				ev.ResponseEnd.GetId().GetBase(),
				ev.ResponseEnd.GetId().GetStream(),
				flow,
				codes.Code(eos.GrpcStatusCode),
				ev.ResponseEnd.GetSinceResponseInit().GetNanos()/1000,
				ev.ResponseEnd.GetResponseBytes(),
				isSecured,
			)

		case *common.Eos_ResetErrorCode:
			return fmt.Sprintf("end id=%d:%d %s reset-error=%+v duration=%dµs response-length=%dB secured=%s",
				ev.ResponseEnd.GetId().GetBase(),
				ev.ResponseEnd.GetId().GetStream(),
				flow,
				eos.ResetErrorCode,
				ev.ResponseEnd.GetSinceResponseInit().GetNanos()/1000,
				ev.ResponseEnd.GetResponseBytes(),
				isSecured,
			)

		default:
			return fmt.Sprintf("end id=%d:%d %s duration=%dµs response-length=%dB secured=%s",
				ev.ResponseEnd.GetId().GetBase(),
				ev.ResponseEnd.GetId().GetStream(),
				flow,
				ev.ResponseEnd.GetSinceResponseInit().GetNanos()/1000,
				ev.ResponseEnd.GetResponseBytes(),
				isSecured,
			)
		}

	default:
		return fmt.Sprintf("unknown %s", flow)
	}
}
