package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/linkerd/linkerd2/controller/api/util"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/addr"
	"github.com/linkerd/linkerd2/pkg/k8s"
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
  * ds/my-daemonset
  * statefulset
  * statefulset/my-statefulset
  * ns/my-ns

  Valid resource types include:
  * daemonsets
  * statefulsets
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

			return requestTapByResourceFromAPI(os.Stdout, cliPublicAPIClient(), req, wide)
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
		_, err = fmt.Fprintln(w, renderTapEvent(event, resource))
		if err != nil {
			return err
		}
	}

	return nil
}

// renderTapEvent renders a Public API TapEvent to a string.
func renderTapEvent(event *pb.TapEvent, resource string) string {
	dst := dst(event)
	src := src(event)

	proxy := "???"
	tls := ""
	switch event.GetProxyDirection() {
	case pb.TapEvent_INBOUND:
		proxy = "in " // A space is added so it aligns with `out`.
		tls = src.tlsStatus()
	case pb.TapEvent_OUTBOUND:
		proxy = "out"
		tls = dst.tlsStatus()
	default:
		// Too old for TLS.
	}

	flow := fmt.Sprintf("proxy=%s %s %s tls=%s",
		proxy,
		src.formatAddr(),
		dst.formatAddr(),
		tls,
	)

	// If `resource` is non-empty, then
	resources := ""
	if resource != "" {
		resources = fmt.Sprintf(
			"%s%s%s",
			src.formatResource(resource),
			dst.formatResource(resource),
			routeLabels(event),
		)
	}

	switch ev := event.GetHttp().GetEvent().(type) {
	case *pb.TapEvent_Http_RequestInit_:
		return fmt.Sprintf("req id=%d:%d %s :method=%s :authority=%s :path=%s%s",
			ev.RequestInit.GetId().GetBase(),
			ev.RequestInit.GetId().GetStream(),
			flow,
			ev.RequestInit.GetMethod().GetRegistered().String(),
			ev.RequestInit.GetAuthority(),
			ev.RequestInit.GetPath(),
			resources,
		)

	case *pb.TapEvent_Http_ResponseInit_:
		return fmt.Sprintf("rsp id=%d:%d %s :status=%d latency=%dµs%s",
			ev.ResponseInit.GetId().GetBase(),
			ev.ResponseInit.GetId().GetStream(),
			flow,
			ev.ResponseInit.GetHttpStatus(),
			ev.ResponseInit.GetSinceRequestInit().GetNanos()/1000,
			resources,
		)

	case *pb.TapEvent_Http_ResponseEnd_:
		switch eos := ev.ResponseEnd.GetEos().GetEnd().(type) {
		case *pb.Eos_GrpcStatusCode:
			return fmt.Sprintf(
				"end id=%d:%d %s grpc-status=%s duration=%dµs response-length=%dB%s",
				ev.ResponseEnd.GetId().GetBase(),
				ev.ResponseEnd.GetId().GetStream(),
				flow,
				codes.Code(eos.GrpcStatusCode),
				ev.ResponseEnd.GetSinceResponseInit().GetNanos()/1000,
				ev.ResponseEnd.GetResponseBytes(),
				resources,
			)

		case *pb.Eos_ResetErrorCode:
			return fmt.Sprintf(
				"end id=%d:%d %s reset-error=%+v duration=%dµs response-length=%dB%s",
				ev.ResponseEnd.GetId().GetBase(),
				ev.ResponseEnd.GetId().GetStream(),
				flow,
				eos.ResetErrorCode,
				ev.ResponseEnd.GetSinceResponseInit().GetNanos()/1000,
				ev.ResponseEnd.GetResponseBytes(),
				resources,
			)

		default:
			return fmt.Sprintf("end id=%d:%d %s duration=%dµs response-length=%dB%s",
				ev.ResponseEnd.GetId().GetBase(),
				ev.ResponseEnd.GetId().GetStream(),
				flow,
				ev.ResponseEnd.GetSinceResponseInit().GetNanos()/1000,
				ev.ResponseEnd.GetResponseBytes(),
				resources,
			)
		}

	default:
		return fmt.Sprintf("unknown %s", flow)
	}
}

// src returns the source peer of a `TapEvent`.
func src(event *pb.TapEvent) peer {
	return peer{
		address:   event.GetSource(),
		labels:    event.GetSourceMeta().GetLabels(),
		direction: "src",
	}
}

// dst returns the destination peer of a `TapEvent`.
func dst(event *pb.TapEvent) peer {
	return peer{
		address:   event.GetDestination(),
		labels:    event.GetDestinationMeta().GetLabels(),
		direction: "dst",
	}
}

type peer struct {
	address   *pb.TcpAddress
	labels    map[string]string
	direction string
}

// formatAddr formats the peer's TCP address for the `src` or `dst` element in
// the tap output corresponding to this peer.
func (p *peer) formatAddr() string {
	return fmt.Sprintf(
		"%s=%s",
		p.direction,
		addr.PublicAddressToString(p.address),
	)
}

// formatResource returns a label describing what Kubernetes resources the peer
// belongs to. If the peer belongs to a resource of kind `resourceKind`, it will
// return a label for that resource; otherwise, it will fall back to the peer's
// pod name. Additionally, if the resource is not of type `namespace`, it will
// also add a label describing the peer's resource.
func (p *peer) formatResource(resourceKind string) string {
	var s string
	if resourceName, exists := p.labels[resourceKind]; exists {
		kind := resourceKind
		if short := k8s.ShortNameFromCanonicalResourceName(resourceKind); short != "" {
			kind = short
		}
		s = fmt.Sprintf(
			" %s_res=%s/%s",
			p.direction,
			kind,
			resourceName,
		)
	} else if pod, hasPod := p.labels[k8s.Pod]; hasPod {
		s = fmt.Sprintf(" %s_pod=%s", p.direction, pod)
	}
	if resourceKind != k8s.Namespace {
		if ns, hasNs := p.labels[k8s.Namespace]; hasNs {
			s += fmt.Sprintf(" %s_ns=%s", p.direction, ns)
		}
	}
	return s
}

func (p *peer) tlsStatus() string {
	return p.labels["tls"]
}

func routeLabels(event *pb.TapEvent) string {
	out := ""
	for key, val := range event.GetRouteMeta().GetLabels() {
		out = fmt.Sprintf("%s rt_%s=%s", out, key, val)
	}

	return out
}
