package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/linkerd/linkerd2/controller/api/util"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/addr"
	runewidth "github.com/mattn/go-runewidth"
	termbox "github.com/nsf/termbox-go"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type topOptions struct {
	namespace   string
	toResource  string
	toNamespace string
	maxRps      float32
	scheme      string
	method      string
	authority   string
	path        string
	hideSources bool
}

type topRequest struct {
	event   *pb.TapEvent
	reqInit *pb.TapEvent_Http_RequestInit
	rspInit *pb.TapEvent_Http_ResponseInit
	rspEnd  *pb.TapEvent_Http_ResponseEnd
}

type topRequestID struct {
	src    string
	dst    string
	stream uint64
}

func (id topRequestID) String() string {
	return fmt.Sprintf("%s->%s(%d)", id.src, id.dst, id.stream)
}

type tableRow struct {
	by          string
	method      string
	source      string
	destination string
	count       int
	best        time.Duration
	worst       time.Duration
	last        time.Duration
	successes   int
	failures    int
}

const headerHeight = 3

var (
	columnNames  = []string{"Source", "Destination", "Method", "Path", "Count", "Best", "Worst", "Last", "Success Rate"}
	columnWidths = []int{23, 23, 10, 37, 6, 6, 6, 6, 3}
)

func newTopOptions() *topOptions {
	return &topOptions{
		namespace:   "default",
		toResource:  "",
		toNamespace: "",
		maxRps:      100.0,
		scheme:      "",
		method:      "",
		authority:   "",
		path:        "",
		hideSources: false,
	}
}

func newCmdTop() *cobra.Command {
	options := newTopOptions()

	cmd := &cobra.Command{
		Use:   "top [flags] (RESOURCE)",
		Short: "Display sorted information about live traffic",
		Long: `Display sorted information about live traffic.

  The RESOURCE argument specifies the target resource(s) to view traffic for:
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
		Example: `  # display traffic for the web deployment in the default namespace
  linkerd top deploy/web

  # display traffic for the web-dlbvj pod in the default namespace
  linkerd top pod/web-dlbvj`,
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

			return getTrafficByResourceFromAPI(os.Stdout, validatedPublicAPIClient(time.Time{}), req, options)
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

	return cmd
}

func getTrafficByResourceFromAPI(w io.Writer, client pb.ApiClient, req *pb.TapByResourceRequest, options *topOptions) error {
	rsp, err := client.TapByResource(context.Background(), req)
	if err != nil {
		return err
	}

	err = termbox.Init()
	if err != nil {
		return err
	}
	defer termbox.Close()

	requestCh := make(chan topRequest, 100)
	done := make(chan struct{})

	go recvEvents(rsp, requestCh, done)
	go pollInput(done)

	renderTable(requestCh, done, !options.hideSources)

	return nil
}

func recvEvents(tapClient pb.Api_TapByResourceClient, requestCh chan<- topRequest, done chan<- struct{}) {
	outstandingRequests := make(map[topRequestID]topRequest)
	for {
		event, err := tapClient.Recv()
		if err == io.EOF {
			fmt.Println("Tap stream terminated")
			close(done)
			return
		}
		if err != nil {
			fmt.Println(err.Error())
			close(done)
			return
		}
		id := topRequestID{
			src: addr.PublicAddressToString(event.GetSource()),
			dst: addr.PublicAddressToString(event.GetDestination()),
		}
		switch ev := event.GetHttp().GetEvent().(type) {
		case *pb.TapEvent_Http_RequestInit_:
			id.stream = ev.RequestInit.GetId().Stream
			outstandingRequests[id] = topRequest{
				event:   event,
				reqInit: ev.RequestInit,
			}

		case *pb.TapEvent_Http_ResponseInit_:
			id.stream = ev.ResponseInit.GetId().Stream
			if req, ok := outstandingRequests[id]; ok {
				req.rspInit = ev.ResponseInit
				outstandingRequests[id] = req
			} else {
				log.Warnf("Got ResponseInit for unknown stream: %s", id)
			}

		case *pb.TapEvent_Http_ResponseEnd_:
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

func pollInput(done chan<- struct{}) {
	for {
		switch ev := termbox.PollEvent(); ev.Type {
		case termbox.EventKey:
			if ev.Ch == 'q' || ev.Key == termbox.KeyCtrlC {
				close(done)
				return
			}
		}
	}
}

func renderTable(requestCh <-chan topRequest, done <-chan struct{}, withSource bool) {
	ticker := time.NewTicker(100 * time.Millisecond)
	var table []tableRow

	for {
		select {
		case <-done:
			return
		case req := <-requestCh:
			tableInsert(&table, req, withSource)
		case <-ticker.C:
			termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
			renderHeaders(withSource)
			renderTableBody(&table, withSource)
			termbox.Flush()
		}
	}
}

func tableInsert(table *[]tableRow, req topRequest, withSource bool) {
	by := req.reqInit.GetPath()
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
		log.Errorf("error parsing duration %v: %s", req.rspEnd.GetSinceRequestInit(), err)
		return
	}
	success := req.rspInit.GetHttpStatus() < 500
	if success {
		switch eos := req.rspEnd.GetEos().GetEnd().(type) {
		case *pb.Eos_GrpcStatusCode:
			success = eos.GrpcStatusCode == 0

		case *pb.Eos_ResetErrorCode:
			success = false
		}
	}

	found := false
	for i, row := range *table {
		if row.by == by && row.method == method && row.destination == destination && (row.source == source || !withSource) {
			(*table)[i].count++
			if latency.Nanoseconds() < row.best.Nanoseconds() {
				(*table)[i].best = latency
			}
			if latency.Nanoseconds() > row.worst.Nanoseconds() {
				(*table)[i].worst = latency
			}
			(*table)[i].last = latency
			if success {
				(*table)[i].successes++
			} else {
				(*table)[i].failures++
			}
			found = true
		}
	}

	if !found {
		successes := 0
		failures := 0
		if success {
			successes++
		} else {
			failures++
		}
		row := tableRow{
			by:          by,
			method:      method,
			source:      source,
			destination: destination,
			count:       1,
			best:        latency,
			worst:       latency,
			last:        latency,
			successes:   successes,
			failures:    failures,
		}
		*table = append(*table, row)
	}
}

func stripPort(address string) string {
	return strings.Split(address, ":")[0]
}

func renderHeaders(withSource bool) {
	tbprint(0, 0, "(press q to quit)")
	x := 0
	for i, header := range columnNames {
		if i == 0 && !withSource {
			continue
		}
		width := columnWidths[i]
		padded := fmt.Sprintf("%-"+strconv.Itoa(width)+"s ", header)
		tbprintBold(x, 2, padded)
		x += width + 1
	}
}

func max(i, j int) int {
	if i > j {
		return i
	}
	return j
}

func renderTableBody(table *[]tableRow, withSource bool) {
	sort.SliceStable(*table, func(i, j int) bool {
		return (*table)[i].count > (*table)[j].count
	})
	adjustedColumnWidths := columnWidths
	for _, row := range *table {
		adjustedColumnWidths[0] = max(adjustedColumnWidths[0], runewidth.StringWidth(row.source))
		adjustedColumnWidths[1] = max(adjustedColumnWidths[1], runewidth.StringWidth(row.destination))
		adjustedColumnWidths[3] = max(adjustedColumnWidths[3], runewidth.StringWidth(row.by))

	}
	for i, row := range *table {
		x := 0
		if withSource {
			tbprint(x, i+headerHeight, row.source)
			x += adjustedColumnWidths[0] + 1
		}
		tbprint(x, i+headerHeight, row.destination)
		x += adjustedColumnWidths[1] + 1
		tbprint(x, i+headerHeight, row.method)
		x += adjustedColumnWidths[2] + 1
		tbprint(x, i+headerHeight, row.by)
		x += adjustedColumnWidths[3] + 1
		tbprint(x, i+headerHeight, strconv.Itoa(row.count))
		x += adjustedColumnWidths[4] + 1
		tbprint(x, i+headerHeight, formatDuration(row.best))
		x += adjustedColumnWidths[5] + 1
		tbprint(x, i+headerHeight, formatDuration(row.worst))
		x += adjustedColumnWidths[6] + 1
		tbprint(x, i+headerHeight, formatDuration(row.last))
		x += adjustedColumnWidths[7] + 1
		successRate := fmt.Sprintf("%.2f%%", 100.0*float32(row.successes)/float32(row.successes+row.failures))
		tbprint(x, i+headerHeight, successRate)
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
