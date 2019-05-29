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
	"github.com/spf13/cobra"
)

type edgesOptions struct {
	namespace    string
	outputFormat string
}

func newEdgesOptions() *edgesOptions {
	return &edgesOptions{
		namespace:    "",
		outputFormat: tableOutput,
	}
}

type indexedEdgeResults struct {
	ix   int
	rows []*pb.Edge
	err  error
}

func newCmdEdges() *cobra.Command {
	options := newEdgesOptions()

	cmd := &cobra.Command{
		Use:   "edges [flags] (RESOURCETYPE)",
		Short: "Display connections between resources, and Linkerd proxy identities",
		Long: `Display connections between resources, and Linkerd proxy identities.

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
  * statefulsets`,
		Example: `  # Get all edges between pods in the test namespace.
  linkerd edges po -n test`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: util.ValidTargets,
		RunE: func(cmd *cobra.Command, args []string) error {
			reqs, err := buildEdgesRequests(args, options)
			if err != nil {
				return fmt.Errorf("Error creating edges request: %s", err)
			}

			// The gRPC client is concurrency-safe, so we can reuse it in all the following goroutines
			// https://github.com/grpc/grpc-go/issues/682
			client := checkPublicAPIClientOrExit()
			c := make(chan indexedEdgeResults, len(reqs))
			for num, req := range reqs {
				go func(num int, req *pb.EdgesRequest) {
					resp, err := requestEdgesFromAPI(client, req)
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

// validateEdgesRequestInputs ensures that the resource type and output format are both supported
// by the edges command, since the edges command does not support all k8s resource types.
func validateEdgesRequestInputs(targets []pb.Resource, options *edgesOptions) error {
	for _, target := range targets {
		if target.Name != "" {
			return fmt.Errorf("Edges cannot be returned for a specific resource name; remove %s from query", target.Name)
		}
		switch target.Type {
		case "authority", "service", "all":
			return fmt.Errorf("Resource type is not supported: %s", target.Type)
		}
	}

	switch options.outputFormat {
	case tableOutput, jsonOutput:
		return nil
	default:
		return fmt.Errorf("--output currently only supports %s and %s", tableOutput, jsonOutput)
	}
}

func buildEdgesRequests(resources []string, options *edgesOptions) ([]*pb.EdgesRequest, error) {
	targets, err := util.BuildResources(options.namespace, resources)

	if err != nil {
		return nil, err
	}
	err = validateEdgesRequestInputs(targets, options)
	if err != nil {
		return nil, err
	}

	requests := make([]*pb.EdgesRequest, 0)
	for _, target := range targets {
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
		rows = append(rows, resp.GetOk().Edges...)
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

func renderEdgeStats(rows []*pb.Edge, options *edgesOptions) string {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 0, padding, ' ', tabwriter.AlignRight)
	writeEdgesToBuffer(rows, w, options)
	w.Flush()

	return renderEdges(buffer, options)
}

type edgeRow struct {
	key    string
	src    string
	dst    string
	client string
	server string
	msg    string
}

const (
	srcHeader    = "SRC"
	dstHeader    = "DST"
	clientHeader = "CLIENT"
	serverHeader = "SERVER"
	msgHeader    = "MSG"
)

func writeEdgesToBuffer(rows []*pb.Edge, w *tabwriter.Writer, options *edgesOptions) {
	maxSrcLength := len(srcHeader)
	maxDstLength := len(dstHeader)
	maxClientLength := len(clientHeader)
	maxServerLength := len(serverHeader)
	maxMsgLength := len(msgHeader)

	edgeRows := []edgeRow{}
	if len(rows) != 0 {
		for _, r := range rows {
			key := r.Src.Name + r.Dst.Name
			clientID := r.ClientId
			serverID := r.ServerId
			msg := r.NoIdentityMsg

			if len(msg) == 0 {
				msg = "-"
			}
			if len(clientID) > 0 {
				parts := strings.Split(clientID, ".")
				clientID = parts[0] + "." + parts[1]
			}
			if len(serverID) > 0 {
				parts := strings.Split(serverID, ".")
				serverID = parts[0] + "." + parts[1]
			}

			row := edgeRow{
				key:    key,
				client: clientID,
				server: serverID,
				msg:    msg,
				src:    r.Src.Name,
				dst:    r.Dst.Name,
			}

			edgeRows = append(edgeRows, row)

			if len(r.Src.Name) > maxSrcLength {
				maxSrcLength = len(r.Src.Name)
			}
			if len(r.Dst.Name) > maxDstLength {
				maxDstLength = len(r.Dst.Name)
			}
			if len(clientID) > maxClientLength {
				maxClientLength = len(clientID)
			}
			if len(serverID) > maxServerLength {
				maxServerLength = len(serverID)
			}
			if len(msg) > maxMsgLength {
				maxMsgLength = len(msg)
			}
		}
	}

	// sorting edgeRows by key for alphabetical listing
	sort.Slice(edgeRows, func(i, j int) bool {
		return edgeRows[i].key < edgeRows[j].key
	})

	switch options.outputFormat {
	case tableOutput:
		if len(edgeRows) == 0 {
			fmt.Fprintln(os.Stderr, "No edges found.")
			os.Exit(0)
		}
		printEdgeTable(edgeRows, w, maxSrcLength, maxDstLength, maxClientLength, maxServerLength, maxMsgLength)
	case jsonOutput:
		printEdgesJSON(edgeRows, w)
	}
}

func printEdgeTable(edgeRows []edgeRow, w *tabwriter.Writer, maxSrcLength, maxDstLength, maxClientLength, maxServerLength, maxMsgLength int) {
	srcTemplate := fmt.Sprintf("%%-%ds", maxSrcLength)
	dstTemplate := fmt.Sprintf("%%-%ds", maxDstLength)
	clientTemplate := fmt.Sprintf("%%-%ds", maxClientLength)
	serverTemplate := fmt.Sprintf("%%-%ds", maxServerLength)
	msgTemplate := fmt.Sprintf("%%-%ds", maxMsgLength)

	headers := []string{
		fmt.Sprintf(srcTemplate, srcHeader),
		fmt.Sprintf(dstTemplate, dstHeader),
		fmt.Sprintf(clientTemplate, clientHeader),
		fmt.Sprintf(serverTemplate, serverHeader),
		fmt.Sprintf(msgTemplate, msgHeader),
	}

	headers[len(headers)-1] = headers[len(headers)-1] + "\t" // trailing \t is required to format last column

	fmt.Fprintln(w, strings.Join(headers, "\t"))

	for _, row := range edgeRows {
		values := make([]interface{}, 0)
		templateString := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t\n", srcTemplate, dstTemplate, clientTemplate, serverTemplate, msgTemplate)

		values = append(values, []interface{}{
			row.src,
			row.dst,
			row.client,
			row.server,
			row.msg,
		}...)

		fmt.Fprintf(w, templateString, values...)

	}
}

func renderEdges(buffer bytes.Buffer, options *edgesOptions) string {
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

type edgesJSONStats struct {
	Src    string `json:"src"`
	Dst    string `json:"dst"`
	Client string `json:"client_id"`
	Server string `json:"server_id"`
	Msg    string `json:"no_tls_reason"`
}

func printEdgesJSON(edgeRows []edgeRow, w *tabwriter.Writer) {
	// avoid nil initialization so that if there are not stats it gets marshalled as an empty array vs null
	entries := []*edgesJSONStats{}

	for _, row := range edgeRows {
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
		fmt.Fprintf(os.Stderr, "Error marshalling JSON: %s\n", err)
		return
	}
	fmt.Fprintf(w, "%s\n", b)
}
