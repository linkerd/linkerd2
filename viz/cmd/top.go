package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/linkerd/linkerd2/pkg/addr"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	metricsAPI "github.com/linkerd/linkerd2/viz/metrics-api"
	metricsPb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	vizpkg "github.com/linkerd/linkerd2/viz/pkg"
	"github.com/linkerd/linkerd2/viz/pkg/api"
	tapPb "github.com/linkerd/linkerd2/viz/tap/gen/tap"
	"github.com/linkerd/linkerd2/viz/tap/pkg"
	runewidth "github.com/mattn/go-runewidth"
	termbox "github.com/nsf/termbox-go"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
)

type topOptions struct {
	namespace     string
	toResource    string
	toNamespace   string
	maxRps        float32
	scheme        string
	method        string
	authority     string
	path          string
	hideSources   bool
	routes        bool
	labelSelector string
}

type topRequest struct {
	event   *tapPb.TapEvent
	reqInit *tapPb.TapEvent_Http_RequestInit
	rspInit *tapPb.TapEvent_Http_ResponseInit
	rspEnd  *tapPb.TapEvent_Http_ResponseEnd
}

type topRequestID struct {
	src    string
	dst    string
	stream uint64
}

func (id topRequestID) String() string {
	return fmt.Sprintf("%s->%s(%d)", id.src, id.dst, id.stream)
}

type tableColumn struct {
	header string
	width  int
	// Columns with key=true will be treated as the primary key for the table.
	// In other words, if two rows have equal values for the key=true columns
	// then those rows will be merged.
	key bool
	// If true, render this column.
	display bool
	// If true, set the width to the widest value in this column.
	flexible   bool
	rightAlign bool
	value      func(tableRow) string
}

type tableRow struct {
	path        string
	method      string
	route       string
	source      string
	destination string
	count       int
	best        time.Duration
	worst       time.Duration
	last        time.Duration
	successes   int
	failures    int
}

func (r tableRow) merge(other tableRow) tableRow {
	r.count += other.count
	if other.best.Nanoseconds() < r.best.Nanoseconds() {
		r.best = other.best
	}
	if other.worst.Nanoseconds() > r.worst.Nanoseconds() {
		r.worst = other.worst
	}
	r.last = other.last
	r.successes += other.successes
	r.failures += other.failures
	return r
}

type column int

const (
	sourceColumn column = iota
	destinationColumn
	methodColumn
	pathColumn
	routeColumn
	countColumn
	bestColumn
	worstColumn
	lastColumn
	successRateColumn

	columnCount
)

type topTable struct {
	columns [columnCount]tableColumn
	rows    []tableRow
}

func newTopTable() *topTable {
	table := topTable{}

	table.columns[sourceColumn] =
		tableColumn{
			header:   "Source",
			width:    23,
			key:      true,
			display:  true,
			flexible: true,
			value: func(r tableRow) string {
				return r.source
			},
		}

	table.columns[destinationColumn] =
		tableColumn{
			header:   "Destination",
			width:    23,
			key:      true,
			display:  true,
			flexible: true,
			value: func(r tableRow) string {
				return r.destination
			},
		}

	table.columns[methodColumn] =
		tableColumn{
			header:   "Method",
			width:    10,
			key:      true,
			display:  true,
			flexible: false,
			value: func(r tableRow) string {
				return r.method
			},
		}

	table.columns[pathColumn] =
		tableColumn{
			header:   "Path",
			width:    37,
			key:      true,
			display:  true,
			flexible: true,
			value: func(r tableRow) string {
				return r.path
			},
		}

	table.columns[routeColumn] =
		tableColumn{
			header:   "Route",
			width:    47,
			key:      false,
			display:  false,
			flexible: true,
			value: func(r tableRow) string {
				return r.route
			},
		}

	table.columns[countColumn] =
		tableColumn{
			header:     "Count",
			width:      6,
			key:        false,
			display:    true,
			flexible:   false,
			rightAlign: true,
			value: func(r tableRow) string {
				return strconv.Itoa(r.count)
			},
		}

	table.columns[bestColumn] =
		tableColumn{
			header:     "Best",
			width:      6,
			key:        false,
			display:    true,
			flexible:   false,
			rightAlign: true,
			value: func(r tableRow) string {
				return formatDuration(r.best)
			},
		}

	table.columns[worstColumn] =
		tableColumn{
			header:     "Worst",
			width:      6,
			key:        false,
			display:    true,
			flexible:   false,
			rightAlign: true,
			value: func(r tableRow) string {
				return formatDuration(r.worst)
			},
		}

	table.columns[lastColumn] =
		tableColumn{
			header:     "Last",
			width:      6,
			key:        false,
			display:    true,
			flexible:   false,
			rightAlign: true,
			value: func(r tableRow) string {
				return formatDuration(r.last)
			},
		}

	table.columns[successRateColumn] =
		tableColumn{
			header:     "Success Rate",
			width:      12,
			key:        false,
			display:    true,
			flexible:   false,
			rightAlign: true,
			value: func(r tableRow) string {
				return fmt.Sprintf("%.2f%%", 100.0*float32(r.successes)/float32(r.successes+r.failures))
			},
		}

	return &table
}

const (
	headerHeight  = 4
	columnSpacing = 2
	xOffset       = 5
)

func newTopOptions() *topOptions {
	return &topOptions{
		toResource:    "",
		toNamespace:   "",
		maxRps:        maxRps,
		scheme:        "",
		method:        "",
		authority:     "",
		path:          "",
		hideSources:   false,
		routes:        false,
		labelSelector: "",
	}
}

// NewCmdTop creates a new cobra command `top` for top functionality
func NewCmdTop() *cobra.Command {
	options := newTopOptions()

	table := newTopTable()

	cmd := &cobra.Command{
		Use:   "top [flags] (RESOURCE)",
		Short: "Display sorted information about live traffic",
		Long: `Display sorted information about live traffic.

  The RESOURCE argument specifies the target resource(s) to view traffic for:
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
		Example: `  # display traffic for the web deployment in the default namespace
  linkerd viz top deploy/web

  # display traffic for the web-dlbvj pod in the default namespace
  linkerd viz top pod/web-dlbvj`,
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
				LabelSelector: options.labelSelector,
			}

			if options.hideSources {
				table.columns[sourceColumn].key = false
				table.columns[sourceColumn].display = false
			}

			if options.routes {
				table.columns[methodColumn].key = false
				table.columns[methodColumn].display = false
				table.columns[pathColumn].key = false
				table.columns[pathColumn].display = false
				table.columns[routeColumn].key = true
				table.columns[routeColumn].display = true
			}

			req, err := pkg.BuildTapByResourceRequest(requestParams)
			if err != nil {
				return err
			}

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			return getTrafficByResourceFromAPI(cmd.Context(), k8sAPI, req, table)
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
	cmd.PersistentFlags().BoolVar(&options.hideSources, "hide-sources", options.hideSources, "Hide the source column")
	cmd.PersistentFlags().BoolVar(&options.routes, "routes", options.routes, "Display data per route instead of per path")
	cmd.PersistentFlags().StringVarP(&options.labelSelector, "selector", "l", options.labelSelector, "Selector (label query) to filter on, supports '=', '==', and '!='")

	return cmd
}

func getTrafficByResourceFromAPI(ctx context.Context, k8sAPI *k8s.KubernetesAPI, req *tapPb.TapByResourceRequest, table *topTable) error {
	reader, body, err := pkg.Reader(ctx, k8sAPI, req)
	if err != nil {
		return err
	}
	defer body.Close()

	err = termbox.Init()
	if err != nil {
		return err
	}
	defer termbox.Close()

	// for event processing:
	// reader ->
	//   recvEvents() ->
	//     eventCh ->
	//       processEvents() ->
	//         requestCh ->
	//           renderTable()
	eventCh := make(chan *tapPb.TapEvent)
	requestCh := make(chan topRequest, 100)

	// for closing:
	// recvEvents() || pollInput() ->
	//   closing ->
	//     done ->
	//       processEvents() && renderTable()
	closing := make(chan struct{}, 1)
	done := make(chan struct{})
	horizontalScroll := make(chan int)

	go pollInput(done, horizontalScroll)
	go recvEvents(reader, eventCh, closing)
	go processEvents(eventCh, requestCh, done)

	go func() {
		<-closing
	}()

	renderTable(table, requestCh, done, horizontalScroll)

	return nil
}

func recvEvents(tapByteStream *bufio.Reader, eventCh chan<- *tapPb.TapEvent, closing chan<- struct{}) {
	for {
		event := &tapPb.TapEvent{}
		err := protohttp.FromByteStreamToProtocolBuffers(tapByteStream, event)
		if err != nil {
			if err == io.EOF {
				fmt.Println("Tap stream terminated")
			} else if !strings.HasSuffix(err.Error(), pkg.ErrClosedResponseBody) {
				fmt.Println(err.Error())
			}

			closing <- struct{}{}
			return
		}

		eventCh <- event
	}
}

func processEvents(eventCh <-chan *tapPb.TapEvent, requestCh chan<- topRequest, done <-chan struct{}) {
	outstandingRequests := make(map[topRequestID]topRequest)

	for {
		select {
		case <-done:
			return
		case event := <-eventCh:
			id := topRequestID{
				src: addr.PublicAddressToString(event.GetSource()),
				dst: addr.PublicAddressToString(event.GetDestination()),
			}
			switch ev := event.GetHttp().GetEvent().(type) {
			case *tapPb.TapEvent_Http_RequestInit_:
				id.stream = ev.RequestInit.GetId().Stream
				outstandingRequests[id] = topRequest{
					event:   event,
					reqInit: ev.RequestInit,
				}

			case *tapPb.TapEvent_Http_ResponseInit_:
				id.stream = ev.ResponseInit.GetId().Stream
				if req, ok := outstandingRequests[id]; ok {
					req.rspInit = ev.ResponseInit
					outstandingRequests[id] = req
				} else {
					log.Warnf("Got ResponseInit for unknown stream: %s", id)
				}

			case *tapPb.TapEvent_Http_ResponseEnd_:
				id.stream = ev.ResponseEnd.GetId().Stream
				if req, ok := outstandingRequests[id]; ok {
					req.rspEnd = ev.ResponseEnd
					requestCh <- req
				} else {
					log.Warnf("Got ResponseEnd for unknown stream: %s", id)
				}
			}
		}
	}
}

func pollInput(done chan<- struct{}, horizontalScroll chan int) {
	for {
		switch ev := termbox.PollEvent(); ev.Type {
		case termbox.EventKey:
			if ev.Ch == 'q' || ev.Key == termbox.KeyCtrlC {
				close(done)
				return
			}
			if ev.Ch == 'a' || ev.Key == termbox.KeyArrowLeft {
				horizontalScroll <- xOffset
			}
			if ev.Ch == 'd' || ev.Key == termbox.KeyArrowRight {
				horizontalScroll <- -xOffset
			}
		}
	}
}

func renderTable(table *topTable, requestCh <-chan topRequest, done <-chan struct{}, horizontalScroll chan int) {
	scrollpos := 0
	ticker := time.NewTicker(100 * time.Millisecond)
	width, _ := termbox.Size()
	tablewidth := table.tableWidthCalc()

	for {
		select {
		case <-done:
			return
		case req := <-requestCh:
			table.insert(req)
		case <-ticker.C:
			termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
			width, _ = termbox.Size()
			table.adjustColumnWidths()
			tablewidth = table.tableWidthCalc()
			table.renderHeaders(scrollpos)
			table.renderBody(scrollpos)
			termbox.Flush()
		case offset := <-horizontalScroll:
			if (offset > 0 && scrollpos < 0) || (offset < 0 && scrollpos > (width-tablewidth)) {
				scrollpos = scrollpos + offset
			}
		}
	}
}

func newRow(req topRequest) (tableRow, error) {
	path := req.reqInit.GetPath()
	route := req.event.GetRouteMeta().GetLabels()["route"]
	if route == "" {
		route = metricsAPI.DefaultRouteName
	}
	method := req.reqInit.GetMethod().GetRegistered().String()
	source := stripPort(addr.PublicAddressToString(req.event.GetSource()))
	if pod := req.event.SourceMeta.Labels["pod"]; pod != "" {
		source = pod
	}
	destination := stripPort(addr.PublicAddressToString(req.event.GetDestination()))
	if pod := req.event.DestinationMeta.Labels["pod"]; pod != "" {
		destination = pod
	}

	latency, err := ptypes.Duration(req.rspEnd.GetSinceRequestInit())
	if err != nil {
		return tableRow{}, fmt.Errorf("error parsing duration %v: %s", req.rspEnd.GetSinceRequestInit(), err)
	}
	// TODO: Once tap events have a classification field, we should use that field
	// instead of determining success here.
	success := req.rspInit.GetHttpStatus() < 500
	if success {
		switch eos := req.rspEnd.GetEos().GetEnd().(type) {
		case *metricsPb.Eos_GrpcStatusCode:
			switch codes.Code(eos.GrpcStatusCode) {
			case codes.Unknown,
				codes.DeadlineExceeded,
				codes.Internal,
				codes.Unavailable,
				codes.DataLoss:
				success = false
			default:
				success = true
			}

		case *metricsPb.Eos_ResetErrorCode:
			success = false
		}
	}

	successes := 0
	failures := 0
	if success {
		successes = 1
	} else {
		failures = 1
	}

	return tableRow{
		path:        path,
		method:      method,
		route:       route,
		source:      source,
		destination: destination,
		best:        latency,
		worst:       latency,
		last:        latency,
		count:       1,
		successes:   successes,
		failures:    failures,
	}, nil
}

func (t *topTable) insert(req topRequest) {
	insert, err := newRow(req)
	if err != nil {
		log.Error(err.Error())
		return
	}

	found := false
	// Search for a matching row
	for i, row := range t.rows {
		match := true
		// If the rows have equal values in all of the key columns, merge them.
		for _, col := range t.columns {
			if col.key {
				if col.value(row) != col.value(insert) {
					match = false
					break
				}
			}
		}
		if match {
			found = true
			t.rows[i] = t.rows[i].merge(insert)
			break
		}
	}
	if !found {
		t.rows = append(t.rows, insert)
	}
}

func stripPort(address string) string {
	return strings.Split(address, ":")[0]
}

func (t *topTable) renderHeaders(scrollpos int) {
	tbprint(0, 0, "(press q to quit)")
	tbprint(0, 1, "(press a/LeftArrowKey to scroll left, d/RightArrowKey to scroll right)")
	x := scrollpos
	for _, col := range t.columns {
		if !col.display {
			continue
		}
		padding := 0
		if col.rightAlign {
			padding = col.width - runewidth.StringWidth(col.header)
		}
		tbprintBold(x+padding, headerHeight-1, col.header)
		x += col.width + columnSpacing
	}
}

func (t *topTable) tableWidthCalc() int {
	tablewidth := 0
	for i := range t.columns {
		tablewidth = tablewidth + t.columns[i].width + columnSpacing
	}
	return tablewidth - columnSpacing
}

func (t *topTable) adjustColumnWidths() {
	for i, col := range t.columns {
		if !col.flexible {
			continue
		}
		t.columns[i].width = runewidth.StringWidth(col.header)
		for _, row := range t.rows {
			cellWidth := runewidth.StringWidth(col.value(row))
			if cellWidth > t.columns[i].width {
				t.columns[i].width = cellWidth
			}
		}
	}
}

func (t *topTable) renderBody(scrollpos int) {
	sort.SliceStable(t.rows, func(i, j int) bool {
		return t.rows[i].count > t.rows[j].count
	})

	for i, row := range t.rows {
		x := scrollpos

		for _, col := range t.columns {
			if !col.display {
				continue
			}
			value := col.value(row)
			padding := 0
			if col.rightAlign {
				padding = col.width - runewidth.StringWidth(value)
			}
			tbprint(x+padding, i+headerHeight, value)
			x += col.width + columnSpacing
		}
	}
}

func tbprint(x, y int, msg string) {
	for _, c := range msg {
		termbox.SetCell(x, y, c, termbox.ColorDefault, termbox.ColorDefault)
		x += runewidth.RuneWidth(c)
	}
}

func tbprintBold(x, y int, msg string) {
	for _, c := range msg {
		termbox.SetCell(x, y, c, termbox.AttrBold, termbox.ColorDefault)
		x += runewidth.RuneWidth(c)
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return d.Round(time.Microsecond).String()
	}
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}
