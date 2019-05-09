package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/linkerd/linkerd2/controller/api/util"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type indexedEdgeResults struct {
	ix   int
	rows []*pb.Edge
	err  error
}

func newCmdEdges() *cobra.Command {
	options := newStatOptions()

	cmd := &cobra.Command{
		Use:   "edges [flags] (RESOURCETYPE)",
		Short: "Display connections between resources, and the identity of their associated Linkerd proxies (if known)",
		Long: `Display connections between resources, and the identity of their associated Linkerd proxies (if known).

  The RESOURCETYPE argument specifies the type of resource to display edges within. A namespace must be specified.

  Examples:
  * deploy
  * ds
  * job
	* po
	* rc
	* sts

  Valid resource types include:
  * daemonsets
  * deployments
  * jobs
  * pods
  * replicationcontrollers
  * statefulsets

If no resource name is specified, displays edges within all resources of the specified RESOURCETYPE`,
		Example: `  # Get all edges between pods in the test namespace.
  linkerd edges deploy -n test`,
		Args:      cobra.MinimumNArgs(1),
		ValidArgs: util.ValidTargets,
		RunE: func(cmd *cobra.Command, args []string) error {
			reqs, err := buildEdgesRequests(args, options)
			if err != nil {
				return fmt.Errorf("error creating metrics request while making edges request: %v", err)
			}

			// The gRPC client is concurrency-safe, so we can reuse it in all the following goroutines
			// https://github.com/grpc/grpc-go/issues/682
			client := checkPublicAPIClientOrExit()
			c := make(chan indexedEdgeResults, len(reqs))
			for num, req := range reqs {
				go func(num int, req *pb.EdgesRequest) {
					resp, err := requestEdgesFromAPI(client, req)
					if err != nil {
						fmt.Println(err)
					}

					rows := edgesRespToRows(resp)

					c <- indexedEdgeResults{num, rows, err}
				}(num, req)
			}

			totalRows := make([]*pb.Edge, 0)
			i := 0
			for res := range c {
				if res.err != nil {
					return res.err
				}
				totalRows = append(totalRows, res.rows...)
				if i++; i == len(reqs) {
					close(c)
				}
			}

			output := renderEdgeStats(totalRows, options)
			_, err = fmt.Print(output)

			return err
		},
	}

	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of the specified resource")
	cmd.PersistentFlags().StringVarP(&options.outputFormat, "output", "o", options.outputFormat, "Output format; one of: \"table\" or \"json\"")
	return cmd
}

func buildEdgesRequests(resources []string, options *statOptions) ([]*pb.EdgesRequest, error) {
	targets, err := util.BuildResources(options.namespace, resources)
	if err != nil {
		return nil, err
	}

	requests := make([]*pb.EdgesRequest, 0)
	for _, target := range targets {
		err = options.validate(target.Type)
		if err != nil {
			return nil, err
		}

		requestParams := util.EdgesRequestParams{
			ResourceType: target.Type,
			Namespace:    options.namespace,
		}

		req, err := util.BuildEdgesRequest(requestParams)
		if err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}
	return requests, nil
}

func edgesRespToRows(resp *pb.EdgesResponse) []*pb.Edge {
	rows := make([]*pb.Edge, 0)
	if resp != nil {
		for _, edgeTable := range resp.GetOk().Edges {
			rows = append(rows, edgeTable)
		}
	}
	return rows
}

func requestEdgesFromAPI(client pb.ApiClient, req *pb.EdgesRequest) (*pb.EdgesResponse, error) {
	resp, err := client.Edges(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("Edges API error: %+v", err)
	}
	if e := resp.GetError(); e != nil {
		return nil, fmt.Errorf("Edges API response error: %+v", e.Error)
	}
	return resp, nil
}

func renderEdgeStats(rows []*pb.Edge, options *statOptions) string {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 0, padding, ' ', tabwriter.AlignRight)
	writeEdgesToBuffer(rows, w, options)
	w.Flush()

	return renderEdges(buffer, &options.statOptionsBase)
}

type edgeRowStats struct {
	src    string
	dst    string
	client string
	server string
	msg    string
}

func writeEdgesToBuffer(rows []*pb.Edge, w *tabwriter.Writer, options *statOptions) {
	edgeTables := make(map[string]*edgeRowStats)
	if len(rows) != 0 {
		for _, r := range rows {
			key := string(r.Dst.Name + r.Src.Name)

			edgeTables[key] = &edgeRowStats{
				client: r.ClientId,
				server: r.ServerId,
				msg:    r.NoIdentityMsg,
				src:    r.Src.Name,
				dst:    r.Dst.Name,
			}
		}
	}
	switch options.outputFormat {
	case tableOutput:
		if len(edgeTables) == 0 {
			fmt.Fprintln(os.Stderr, "No edges found.")
			os.Exit(0)
		}
		printSingleEdgeTable(edgeTables, w)
	case jsonOutput:
		printEdgesJSON(edgeTables, w)
	}
}

// returns the length of the longest src name
func srcWidth(stats map[string]*edgeRowStats) int {
	maxLength := 0
	for _, row := range stats {
		if len(row.src) > maxLength {
			maxLength = len(row.src)
		}
	}
	return maxLength
}

func printSingleEdgeTable(edges map[string]*edgeRowStats, w *tabwriter.Writer) {
	// template for left-aligning the src column
	srcTemplate := fmt.Sprintf("%%-%ds", srcWidth(edges))

	headers := []string{
		fmt.Sprintf(srcTemplate, "SRC"),
	}
	headers = append(headers, []string{
		"DST",
		"CLIENT",
		"SERVER",
		"MSG",
	}...)

	headers[len(headers)-1] = headers[len(headers)-1] + "\t" // trailing \t is required to format last column

	fmt.Fprintln(w, strings.Join(headers, "\t"))

	sortedKeys := sortEdgesKeys(edges)
	for _, key := range sortedKeys {
		values := make([]interface{}, 0)
		templateString := srcTemplate + "\t%s\t%s\t%s\t%s\t\n"

		if edges[key].msg == "" {
			edges[key].msg = "-"
		}

		values = append(values, []interface{}{
			edges[key].src,
			edges[key].dst,
			edges[key].client,
			edges[key].server,
			edges[key].msg,
		}...)

		fmt.Fprintf(w, templateString, values...)

	}
}

func renderEdges(buffer bytes.Buffer, options *statOptionsBase) string {
	var out string
	switch options.outputFormat {
	case jsonOutput:
		out = buffer.String()
	default:
		// strip left padding on the first column
		out = string(buffer.Bytes()[padding:])
		out = strings.Replace(out, "\n"+strings.Repeat(" ", padding), "\n", -1)
	}

	return out
}

func sortEdgesKeys(stats map[string]*edgeRowStats) []string {
	var sortedKeys []string
	for key := range stats {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)
	return sortedKeys
}

type edgesJSONStats struct {
	Src    string `json:"src"`
	Dst    string `json:"dst"`
	Client string `json:"client_id"`
	Server string `json:"server_id"`
	Msg    string `json:"no_tls_reason"`
}

func printEdgesJSON(edgeTables map[string]*edgeRowStats, w *tabwriter.Writer) {
	// avoid nil initialization so that if there are not stats it gets marshalled as an empty array vs null
	entries := []*edgesJSONStats{}

	for _, row := range edgeTables {
		entry := &edgesJSONStats{
			Src:    row.src,
			Dst:    row.dst,
			Client: row.client,
			Server: row.server,
			Msg:    row.msg}
		entries = append(entries, entry)
	}

	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		log.Error(err.Error())
		return
	}
	fmt.Fprintf(w, "%s\n", b)
}
