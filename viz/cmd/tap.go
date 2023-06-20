package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/golang/protobuf/ptypes/duration"
	netPb "github.com/linkerd/linkerd2/controller/gen/common/net"
	"github.com/linkerd/linkerd2/pkg/addr"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	metricsPb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/linkerd/linkerd2/viz/pkg/api"
	hc "github.com/linkerd/linkerd2/viz/pkg/healthcheck"
	"github.com/linkerd/linkerd2/viz/pkg/jsonpath"
	vizutil "github.com/linkerd/linkerd2/viz/pkg/util"
	tapPb "github.com/linkerd/linkerd2/viz/tap/gen/tap"
	"github.com/linkerd/linkerd2/viz/tap/pkg"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
)

type renderTapEventFunc func(*tapPb.TapEvent, ...renderOptions) string

type tapOptions struct {
	namespace     string
	toResource    string
	toNamespace   string
	maxRps        float32
	scheme        string
	method        string
	authority     string
	path          string
	output        string
	labelSelector string
}

type endpoint struct {
	IP       string            `json:"ip"`
	Port     uint32            `json:"port"`
	Metadata map[string]string `json:"metadata"`
}

type streamID struct {
	Base   uint32 `json:"base"`
	Stream uint64 `json:"stream"`
}

type metadata interface {
	isMetadata()
}

type metadataStr struct {
	Name     string `json:"name"`
	ValueStr string `json:"valueStr"`
}

func (*metadataStr) isMetadata() {}

type metadataBin struct {
	Name     string `json:"name"`
	ValueBin []byte `json:"valueBin"`
}

func (*metadataBin) isMetadata() {}

type requestInitEvent struct {
	ID        *streamID  `json:"id"`
	Method    string     `json:"method"`
	Scheme    string     `json:"scheme"`
	Authority string     `json:"authority"`
	Path      string     `json:"path"`
	Headers   []metadata `json:"headers"`
}

type responseInitEvent struct {
	ID               *streamID          `json:"id"`
	SinceRequestInit *duration.Duration `json:"sinceRequestInit"`
	HTTPStatus       uint32             `json:"httpStatus"`
	Headers          []metadata         `json:"headers"`
}

type responseEndEvent struct {
	ID                *streamID          `json:"id"`
	SinceRequestInit  *duration.Duration `json:"sinceRequestInit"`
	SinceResponseInit *duration.Duration `json:"sinceResponseInit"`
	ResponseBytes     uint64             `json:"responseBytes"`
	Trailers          []metadata         `json:"trailers"`
	GrpcStatusCode    uint32             `json:"grpcStatusCode"`
	ResetErrorCode    uint32             `json:"resetErrorCode,omitempty"`
}

// Private type used for displaying JSON encoded tap events
type tapEvent struct {
	Source            *endpoint          `json:"source"`
	Destination       *endpoint          `json:"destination"`
	RouteMeta         map[string]string  `json:"routeMeta"`
	ProxyDirection    string             `json:"proxyDirection"`
	RequestInitEvent  *requestInitEvent  `json:"requestInitEvent,omitempty"`
	ResponseInitEvent *responseInitEvent `json:"responseInitEvent,omitempty"`
	ResponseEndEvent  *responseEndEvent  `json:"responseEndEvent,omitempty"`
}

func newTapOptions() *tapOptions {
	return &tapOptions{
		toResource:    "",
		toNamespace:   "",
		maxRps:        maxRps,
		scheme:        "",
		method:        "",
		authority:     "",
		path:          "",
		output:        "",
		labelSelector: "",
	}
}

type renderFilter struct {
	JsonPath string
}

type renderOptions func(f *renderFilter)

func WithJsonPath(jsonPath string) renderOptions {
	return func(r *renderFilter) {
		r.JsonPath = jsonPath
	}
}

func (o *tapOptions) validate() error {
	if o.output == "" || o.output == wideOutput || o.output == jsonOutput || strings.HasPrefix(o.output, jsonPathOutput) {
		return nil
	}

	return fmt.Errorf("output format \"%s\" not recognized", o.output)
}

// NewCmdTap creates a new cobra command `tap` for tap functionality
func NewCmdTap() *cobra.Command {
	options := newTapOptions()

	cmd := &cobra.Command{
		Use:   "tap [flags] (RESOURCE)",
		Short: "Listen to a traffic stream",
		Long: `Listen to a traffic stream.

  The RESOURCE argument specifies the target resource(s) to tap:
  (TYPE [NAME] | TYPE/NAME)

  Examples:
  * cronjob/my-cronjob
  * deploy
  * deploy/my-deploy
  * deploy my-deploy
  * ds/my-daemonset
  * job/my-job
  * ns/my-ns
  * rs
  * rs/my-replicaset
  * sts
  * sts/my-statefulset

  Valid resource types include:
  * cronjobs
  * daemonsets
  * deployments
  * jobs
  * namespaces
  * pods
  * replicasets
  * replicationcontrollers
  * statefulsets
  * services (only supported as a --to resource)`,
		Example: `  # tap the web deployment in the default namespace
  linkerd viz tap deploy/web

  # tap the web-dlbvj pod in the default namespace
  linkerd viz tap pod/web-dlbvj

  # tap the test namespace, filter by request to prod namespace
  linkerd viz tap ns/test --to ns/prod`,
		Args: cobra.RangeArgs(1, 2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			// This command requires at most two arguments if we already have
			// two after requesting autocompletion i.e. [tab][tab]
			// skip running validArgsFunction
			if len(args) > 1 {
				return nil, cobra.ShellCompDirectiveError
			}

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}

			if options.namespace == "" {
				options.namespace = pkgcmd.GetDefaultNamespace(kubeconfigPath, kubeContext)
			}

			cc := k8s.NewCommandCompletion(k8sAPI, options.namespace)

			results, err := cc.Complete(args, toComplete)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}

			return results, cobra.ShellCompDirectiveDefault
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.namespace == "" {
				options.namespace = pkgcmd.GetDefaultNamespace(kubeconfigPath, kubeContext)
			}

			api.CheckClientOrExit(hc.VizOptions{
				Options: &healthcheck.Options{
					ControlPlaneNamespace: controlPlaneNamespace,
					KubeConfig:            kubeconfigPath,
					Impersonate:           impersonate,
					ImpersonateGroup:      impersonateGroup,
					KubeContext:           kubeContext,
					APIAddr:               apiAddr,
				},
				VizNamespaceOverride: vizNamespace,
			})

			requestParams := pkg.TapRequestParams{
				Resource:      strings.Join(args, "/"),
				Namespace:     options.namespace,
				ToResource:    options.toResource,
				ToNamespace:   options.toNamespace,
				MaxRps:        options.maxRps,
				Scheme:        options.scheme,
				Method:        options.method,
				Authority:     options.authority,
				Path:          options.path,
				Extract:       options.output == jsonOutput,
				LabelSelector: options.labelSelector,
			}

			err := options.validate()
			if err != nil {
				return fmt.Errorf("validation error when executing tap command: %w", err)
			}

			req, err := pkg.BuildTapByResourceRequest(requestParams)
			if err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}

			err = requestTapByResourceFromAPI(cmd.Context(), os.Stdout, k8sAPI, req, options)
			if err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}

			return nil
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
		fmt.Sprintf("Output format. One of: \"%s\", \"%s\", \"%s\"", wideOutput, jsonOutput, jsonPathOutput))
	cmd.PersistentFlags().StringVarP(&options.labelSelector, "selector", "l", options.labelSelector,
		"Selector (label query) to filter on, supports '=', '==', and '!='")

	pkgcmd.ConfigureNamespaceFlagCompletion(
		cmd, []string{"namespace", "to-namespace"},
		kubeconfigPath, impersonate, impersonateGroup, kubeContext)
	return cmd
}

func requestTapByResourceFromAPI(ctx context.Context, w io.Writer, k8sAPI *k8s.KubernetesAPI, req *tapPb.TapByResourceRequest, options *tapOptions) error {
	reader, body, err := pkg.Reader(ctx, k8sAPI, req)
	if err != nil {
		return err
	}
	defer body.Close()

	return writeTapEventsToBuffer(w, reader, options)
}

func writeTapEventsToBuffer(w io.Writer, tapByteStream *bufio.Reader, options *tapOptions) error {
	output := options.output

	switch {
	case output == "":
		return renderTapEvents(tapByteStream, w, renderTapEvent)
	case output == wideOutput:
		return renderTapEvents(tapByteStream, w, renderTapEventWide)
	case output == jsonOutput:
		return renderTapEvents(tapByteStream, w, renderTapEventJSON)
	case strings.HasPrefix(output, jsonPathOutput):
		jPathFilter, err := jsonpath.GetJsonPathFlagVal(output)
		if err != nil {
			return err
		}
		return renderTapEvents(tapByteStream, w, renderTapEventJSON, WithJsonPath(jPathFilter))
	default:
		return fmt.Errorf("unknown output format: %q", options.output)
	}
}

func renderTapEvents(tapByteStream *bufio.Reader, w io.Writer, render renderTapEventFunc, opts ...renderOptions) error {
	for {
		log.Debug("Waiting for data...")
		event := tapPb.TapEvent{}
		err := protohttp.FromByteStreamToProtocolBuffers(tapByteStream, &event)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			fmt.Fprintln(os.Stderr, err)
			break
		}
		_, err = fmt.Fprintln(w, render(&event, opts...))
		if err != nil {
			return err
		}
	}

	return nil
}

func renderTapEventWide(event *tapPb.TapEvent, _ ...renderOptions) string {
	dst := dst(event)
	src := src(event)

	out := []string{renderTapEvent(event)}
	out = append(out, src.formatResource()...)
	out = append(out, dst.formatResource()...)
	out = append(out, routeLabels(event)...)
	return strings.Join(out, " ")
}

// renderTapEvent renders a Public API TapEvent to a string.
func renderTapEvent(event *tapPb.TapEvent, _ ...renderOptions) string {
	dst := dst(event)
	src := src(event)

	proxy := "???"
	tls := ""
	switch event.GetProxyDirection() {
	case tapPb.TapEvent_INBOUND:
		proxy = "in " // A space is added so it aligns with `out`.
		tls = src.tlsStatus()
	case tapPb.TapEvent_OUTBOUND:
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

	switch ev := event.GetHttp().GetEvent().(type) {
	case *tapPb.TapEvent_Http_RequestInit_:
		return fmt.Sprintf("req id=%d:%d %s :method=%s :authority=%s :path=%s",
			ev.RequestInit.GetId().GetBase(),
			ev.RequestInit.GetId().GetStream(),
			flow,
			vizutil.HTTPMethodToString(ev.RequestInit.GetMethod()),
			ev.RequestInit.GetAuthority(),
			ev.RequestInit.GetPath(),
		)

	case *tapPb.TapEvent_Http_ResponseInit_:
		return fmt.Sprintf("rsp id=%d:%d %s :status=%d latency=%dµs",
			ev.ResponseInit.GetId().GetBase(),
			ev.ResponseInit.GetId().GetStream(),
			flow,
			ev.ResponseInit.GetHttpStatus(),
			ev.ResponseInit.GetSinceRequestInit().AsDuration().Microseconds(),
		)

	case *tapPb.TapEvent_Http_ResponseEnd_:
		switch eos := ev.ResponseEnd.GetEos().GetEnd().(type) {
		case *metricsPb.Eos_GrpcStatusCode:
			return fmt.Sprintf(
				"end id=%d:%d %s grpc-status=%s duration=%dµs response-length=%dB",
				ev.ResponseEnd.GetId().GetBase(),
				ev.ResponseEnd.GetId().GetStream(),
				flow,
				codes.Code(eos.GrpcStatusCode),
				ev.ResponseEnd.GetSinceResponseInit().AsDuration().Microseconds(),
				ev.ResponseEnd.GetResponseBytes(),
			)

		case *metricsPb.Eos_ResetErrorCode:
			return fmt.Sprintf(
				"end id=%d:%d %s reset-error=%+v duration=%dµs response-length=%dB",
				ev.ResponseEnd.GetId().GetBase(),
				ev.ResponseEnd.GetId().GetStream(),
				flow,
				eos.ResetErrorCode,
				ev.ResponseEnd.GetSinceResponseInit().AsDuration().Microseconds(),
				ev.ResponseEnd.GetResponseBytes(),
			)

		default:
			return fmt.Sprintf("end id=%d:%d %s duration=%dµs response-length=%dB",
				ev.ResponseEnd.GetId().GetBase(),
				ev.ResponseEnd.GetId().GetStream(),
				flow,
				ev.ResponseEnd.GetSinceResponseInit().AsDuration().Microseconds(),
				ev.ResponseEnd.GetResponseBytes(),
			)
		}

	default:
		return fmt.Sprintf("unknown %s", flow)
	}
}

// renderTapEventJSON renders a Public API TapEvent to a string in JSON format.
func renderTapEventJSON(event *tapPb.TapEvent, opts ...renderOptions) string {
	filter := &renderFilter{}
	for _, opt := range opts {
		opt(filter)
	}
	m := mapPublicToDisplayTapEvent(event)
	if filter.JsonPath != "" {
		filteredJson, err := jsonpath.GetJsonFilteredByJPath(m, filter.JsonPath)
		if err != nil {
			return err.Error()
		}
		return filteredJson[0]
	}
	e, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Sprintf("{\"error marshalling JSON\": \"%s\"}", err)
	}
	return string(e)
}

// Map public API `TapEvent`s to `displayTapEvent`s
func mapPublicToDisplayTapEvent(event *tapPb.TapEvent) *tapEvent {
	// Map source endpoint
	sip := addr.PublicIPToString(event.GetSource().GetIp())
	src := &endpoint{
		IP:       sip,
		Port:     event.GetSource().GetPort(),
		Metadata: event.GetSourceMeta().GetLabels(),
	}

	// Map destination endpoint
	dip := addr.PublicIPToString(event.GetDestination().GetIp())
	dst := &endpoint{
		IP:       dip,
		Port:     event.GetDestination().GetPort(),
		Metadata: event.GetDestinationMeta().GetLabels(),
	}

	return &tapEvent{
		Source:            src,
		Destination:       dst,
		RouteMeta:         event.GetRouteMeta().GetLabels(),
		ProxyDirection:    event.GetProxyDirection().String(),
		RequestInitEvent:  getRequestInitEvent(event.GetHttp()),
		ResponseInitEvent: getResponseInitEvent(event.GetHttp()),
		ResponseEndEvent:  getResponseEndEvent(event.GetHttp()),
	}
}

// Attempt to map a `TapEvent_Http_RequestInit event to a `requestInitEvent`
func getRequestInitEvent(pubEv *tapPb.TapEvent_Http) *requestInitEvent {
	reqI := pubEv.GetRequestInit()
	if reqI == nil {
		return nil
	}
	sid := &streamID{
		Base:   reqI.GetId().GetBase(),
		Stream: reqI.GetId().GetStream(),
	}
	return &requestInitEvent{
		ID:        sid,
		Method:    formatMethod(reqI.GetMethod()),
		Scheme:    formatScheme(reqI.GetScheme()),
		Authority: reqI.GetAuthority(),
		Path:      reqI.GetPath(),
		Headers:   formatHeadersTrailers(reqI.GetHeaders()),
	}
}

func formatMethod(m *metricsPb.HttpMethod) string {
	if x, ok := m.GetType().(*metricsPb.HttpMethod_Registered_); ok {
		return x.Registered.String()
	}
	if s, ok := m.GetType().(*metricsPb.HttpMethod_Unregistered); ok {
		return s.Unregistered
	}
	return ""
}

func formatScheme(s *metricsPb.Scheme) string {
	if x, ok := s.GetType().(*metricsPb.Scheme_Registered_); ok {
		return x.Registered.String()
	}
	if str, ok := s.GetType().(*metricsPb.Scheme_Unregistered); ok {
		return str.Unregistered
	}
	return ""
}

// Attempt to map a `TapEvent_Http_ResponseInit` event to a `responseInitEvent`
func getResponseInitEvent(pubEv *tapPb.TapEvent_Http) *responseInitEvent {
	resI := pubEv.GetResponseInit()
	if resI == nil {
		return nil
	}
	sid := &streamID{
		Base:   resI.GetId().GetBase(),
		Stream: resI.GetId().GetStream(),
	}
	return &responseInitEvent{
		ID:               sid,
		SinceRequestInit: resI.GetSinceRequestInit(),
		HTTPStatus:       resI.GetHttpStatus(),
		Headers:          formatHeadersTrailers(resI.GetHeaders()),
	}
}

// Attempt to map a `TapEvent_Http_ResponseEnd` event to a `responseEndEvent`
func getResponseEndEvent(pubEv *tapPb.TapEvent_Http) *responseEndEvent {
	resE := pubEv.GetResponseEnd()
	if resE == nil {
		return nil
	}
	sid := &streamID{
		Base:   resE.GetId().GetBase(),
		Stream: resE.GetId().GetStream(),
	}
	return &responseEndEvent{
		ID:                sid,
		SinceRequestInit:  resE.GetSinceRequestInit(),
		SinceResponseInit: resE.GetSinceResponseInit(),
		ResponseBytes:     resE.GetResponseBytes(),
		Trailers:          formatHeadersTrailers(resE.GetTrailers()),
		GrpcStatusCode:    resE.GetEos().GetGrpcStatusCode(),
		ResetErrorCode:    resE.GetEos().GetResetErrorCode(),
	}
}

func formatHeadersTrailers(hs *metricsPb.Headers) []metadata {
	var fm []metadata
	for _, h := range hs.GetHeaders() {
		switch h.GetValue().(type) {
		case *metricsPb.Headers_Header_ValueStr:
			fht := &metadataStr{Name: h.GetName(), ValueStr: h.GetValueStr()}
			fm = append(fm, fht)
			continue
		case *metricsPb.Headers_Header_ValueBin:
			fht := &metadataBin{Name: h.GetName(), ValueBin: h.GetValueBin()}
			fm = append(fm, fht)
			continue
		}
	}
	return fm
}

// src returns the source peer of a `TapEvent`.
func src(event *tapPb.TapEvent) peer {
	return peer{
		address:   event.GetSource(),
		labels:    event.GetSourceMeta().GetLabels(),
		direction: "src",
	}
}

// dst returns the destination peer of a `TapEvent`.
func dst(event *tapPb.TapEvent) peer {
	return peer{
		address:   event.GetDestination(),
		labels:    event.GetDestinationMeta().GetLabels(),
		direction: "dst",
	}
}

type peer struct {
	address   *netPb.TcpAddress
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

// formatResource returns the peer's labels formatted and sorted.
func (p *peer) formatResource() []string {
	labels := []string{}
	for k, v := range p.labels {
		labels = append(labels, fmt.Sprintf("%s_%s=%s", p.direction, k, v))
	}
	sort.Strings(labels)
	return labels
}

func (p *peer) tlsStatus() string {
	return p.labels["tls"]
}

func routeLabels(event *tapPb.TapEvent) []string {
	out := []string{}
	for key, val := range event.GetRouteMeta().GetLabels() {
		out = append(out, fmt.Sprintf("rt_%s=%s", key, val))
	}
	return out
}
