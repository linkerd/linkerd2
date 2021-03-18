package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/golang/protobuf/ptypes"
	"github.com/linkerd/linkerd2/pkg/addr"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	vizpkg "github.com/linkerd/linkerd2/viz/pkg"
	"github.com/linkerd/linkerd2/viz/pkg/api"
	tapPb "github.com/linkerd/linkerd2/viz/tap/gen/tap"
	"github.com/linkerd/linkerd2/viz/tap/pkg"
	"github.com/rivo/tview"
	"github.com/spf13/cobra"
)

type eventLog struct {
	app     *tview.Application
	table   *tview.Table
	details *tview.TextView
	events  []topRequest
}

// NewCmdTapShark creates a new cobra command `tap` for tap functionality
func NewCmdTapShark() *cobra.Command {
	options := newTapOptions()

	cmd := &cobra.Command{
		Use:   "tapshark [flags] (RESOURCE)",
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
  linkerd viz tapshark deploy/web

  # tap the web-dlbvj pod in the default namespace
  linkerd viz tapshark pod/web-dlbvj

  # tap the test namespace, filter by request to prod namespace
  linkerd viz tapshark ns/test --to ns/prod`,
		Args:      cobra.RangeArgs(1, 2),
		ValidArgs: vizpkg.ValidTargets,
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.namespace == "" {
				options.namespace = pkgcmd.GetDefaultNamespace(kubeconfigPath, kubeContext)
			}

			api.CheckClientOrExit(healthcheck.Options{
				ControlPlaneNamespace: controlPlaneNamespace,
				KubeConfig:            kubeconfigPath,
				Impersonate:           impersonate,
				ImpersonateGroup:      impersonateGroup,
				KubeContext:           kubeContext,
				APIAddr:               apiAddr,
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
				Extract:       true,
				LabelSelector: options.labelSelector,
			}

			err := options.validate()
			if err != nil {
				return fmt.Errorf("validation error when executing tap command: %v", err)
			}

			req, err := pkg.BuildTapByResourceRequest(requestParams)
			if err != nil {
				fmt.Fprint(os.Stderr, err.Error())
				os.Exit(1)
			}

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				fmt.Fprint(os.Stderr, err.Error())
				os.Exit(1)
			}

			headers := []string{"FROM", pad("POD"), pad("TO"), pad("VERB"), pad("PATH"), pad("STATUS"), "LATENCY"}

			table := tview.NewTable().SetFixed(1, 0).SetSelectable(true, false)
			for i, header := range headers {
				cell := tview.NewTableCell(header)
				cell.SetAttributes(tcell.AttrBold)
				table.SetCell(0, i, cell)
			}

			done := make(chan struct{})

			details := tview.NewTextView().SetDynamicColors(true)

			grid := tview.NewGrid().SetSize(2, 1, -1, -1).
				AddItem(table, 0, 0, 1, 1, 0, 0, true).
				AddItem(details, 1, 0, 1, 1, 0, 0, false).
				SetBorders(true)
			grid.SetTitle(strings.Join(os.Args, " "))

			app := tview.NewApplication().SetRoot(grid, true)
			app.SetInputCapture(
				func(event *tcell.EventKey) *tcell.EventKey {
					if event.Key() == tcell.KeyTAB {
						if table.HasFocus() {
							app.SetFocus(details)
						} else {
							app.SetFocus(table)
						}
						return nil
					}
					return event
				})

			eventLog := &eventLog{
				app:     app,
				details: details,
				table:   table,
				events:  []topRequest{},
			}

			table.SetSelectedFunc(eventLog.selectionChanged)

			go eventLog.processTapEvents(cmd.Context(), k8sAPI, req, options, done)

			if err := app.Run(); err != nil {
				panic(err)
			}

			done <- struct{}{}

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
	cmd.PersistentFlags().StringVarP(&options.labelSelector, "selector", "l", options.labelSelector,
		"Selector (label query) to filter on, supports '=', '==', and '!='")

	return cmd
}

func (el *eventLog) processTapEvents(ctx context.Context, k8sAPI *k8s.KubernetesAPI, req *tapPb.TapByResourceRequest, options *tapOptions, done <-chan struct{}) {
	reader, body, err := pkg.Reader(ctx, k8sAPI, req)
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		return
	}
	defer body.Close()

	eventCh := make(chan *tapPb.TapEvent)
	requestCh := make(chan topRequest, 100)

	closing := make(chan struct{}, 1)

	go recvEvents(reader, eventCh, closing)
	go processEvents(eventCh, requestCh, done)

	go func() {
		<-closing
	}()

	for {
		select {
		case <-done:
			return
		case req := <-requestCh:

			el.events = append(el.events, req)
			row := len(el.events)

			from, pod, to := fromPodTo(req)
			verb := req.reqInit.GetMethod().GetRegistered().String()
			path := req.reqInit.GetPath()
			status := fmt.Sprintf("%d", req.rspInit.GetHttpStatus())
			latency := latency(req)

			el.app.QueueUpdateDraw(func() {

				el.table.SetCellSimple(row, 0, from)
				el.table.SetCellSimple(row, 1, pad(pod))
				el.table.SetCellSimple(row, 2, pad(to))
				el.table.SetCellSimple(row, 3, pad(verb))
				el.table.SetCellSimple(row, 4, pad(path))
				el.table.SetCellSimple(row, 5, pad(status))
				el.table.SetCellSimple(row, 6, latency)
			})
		}
	}

}

func (el *eventLog) selectionChanged(row, column int) {
	if row == 0 {
		el.details.Clear()
		return
	}
	req := el.events[row-1]
	from, pod, to := fromPodTo(req)
	el.details.Clear()

	fieldTemplate := "[::b]%s:[-:-:-] %s\n"

	fmt.Fprintf(el.details, fieldTemplate, "Pod", pod)
	if from != "" {
		fmt.Fprintf(el.details, fieldTemplate, "From", from)
	}
	if to != "" {
		fmt.Fprintf(el.details, fieldTemplate, "To", to)
	}
	fmt.Fprintf(el.details, fieldTemplate, "Scheme", req.reqInit.GetScheme().GetRegistered().String())
	fmt.Fprintf(el.details, fieldTemplate, "Verb", req.reqInit.GetMethod().GetRegistered().String())
	fmt.Fprintf(el.details, fieldTemplate, "Path", req.reqInit.GetPath())
	fmt.Fprintf(el.details, fieldTemplate, "Authority", req.reqInit.GetAuthority())
	fmt.Fprintf(el.details, fieldTemplate, "Request Headers", "")
	for _, header := range req.reqInit.GetHeaders().GetHeaders() {
		fmt.Fprintf(el.details, "\t%s: %s\n", header.GetName(), header.GetValueStr())
	}
	fmt.Fprintf(el.details, fieldTemplate, "Latency", latency(req))
	fmt.Fprintf(el.details, fieldTemplate, "Status", fmt.Sprintf("%d", req.rspInit.GetHttpStatus()))

	var duration string
	d, err := ptypes.Duration(req.rspEnd.GetSinceResponseInit())
	if err == nil {
		duration = d.String()
	}

	fmt.Fprintf(el.details, fieldTemplate, "Duration", duration)
	fmt.Fprintf(el.details, fieldTemplate, "Response Headers", "")
	for _, header := range req.rspInit.GetHeaders().GetHeaders() {
		fmt.Fprintf(el.details, "\t%s: %s\n", header.GetName(), header.GetValueStr())
	}
	fmt.Fprintf(el.details, fieldTemplate, "Response Trailers", "")
	for _, header := range req.rspEnd.Trailers.GetHeaders() {
		fmt.Fprintf(el.details, "\t%s: %s\n", header.GetName(), header.GetValueStr())
	}
	el.details.ScrollToBeginning()
}

func pad(s string) string {
	return fmt.Sprintf(" %s ", s)
}

func fromPodTo(req topRequest) (string, string, string) {
	source := stripPort(addr.PublicAddressToString(req.event.GetSource()))
	if pod := req.event.SourceMeta.Labels["pod"]; pod != "" {
		source = pod
	}
	destination := stripPort(addr.PublicAddressToString(req.event.GetDestination()))
	if pod := req.event.DestinationMeta.Labels["pod"]; pod != "" {
		destination = pod
	}
	var from, pod, to string
	if req.event.GetProxyDirection() == tapPb.TapEvent_INBOUND {
		from = source
		pod = destination
	} else if req.event.GetProxyDirection() == tapPb.TapEvent_OUTBOUND {
		pod = source
		to = destination
	}
	return from, pod, to
}

func latency(req topRequest) string {
	latency, err := ptypes.Duration(req.rspEnd.GetSinceRequestInit())
	if err != nil {
		return ""
	}
	return latency.String()
}
