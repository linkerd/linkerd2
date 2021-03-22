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

	"github.com/fatih/color"
	coreUtil "github.com/linkerd/linkerd2/controller/api/util"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/linkerd/linkerd2/viz/metrics-api/util"
	"github.com/linkerd/linkerd2/viz/pkg"
	"github.com/linkerd/linkerd2/viz/pkg/api"
	"github.com/spf13/cobra"
)

var (
	okStatus = color.New(color.FgGreen, color.Bold).SprintFunc()("\u221A") // √

)

type edgesOptions struct {
	namespace     string
	outputFormat  string
	allNamespaces bool
}

func newEdgesOptions() *edgesOptions {
	return &edgesOptions{
		outputFormat:  tableOutput,
		allNamespaces: false,
	}
}

type indexedEdgeResults struct {
	ix   int
	rows []*pb.Edge
	err  error
}

// NewCmdEdges creates a new cobra command `edges` for edges functionality
func NewCmdEdges() *cobra.Command {
	options := newEdgesOptions()

	cmd := &cobra.Command{
		Use:   "edges [flags] (RESOURCETYPE)",
		Short: "Display connections between resources, and Linkerd proxy identities",
		Long: `Display connections between resources, and Linkerd proxy identities.

  The RESOURCETYPE argument specifies the type of resource to display edges within.

  Examples:
  * cronjob
  * deploy
  * ds
  * job
  * po
  * rc
  * rs
  * sts

  Valid resource types include:
  * cronjobs
  * daemonsets
  * deployments
  * jobs
  * pods
  * replicasets
  * replicationcontrollers
  * statefulsets`,
		Example: `  # Get all edges between pods that either originate from or terminate in the test namespace.
  linkerd viz edges po -n test

  # Get all edges between pods that either originate from or terminate in the default namespace.
  linkerd viz edges po

  # Get all edges between pods in all namespaces.
  linkerd viz edges po --all-namespaces`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: pkg.ValidTargets,
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.namespace == "" {
				options.namespace = pkgcmd.GetDefaultNamespace(kubeconfigPath, kubeContext)
			}

			reqs, err := buildEdgesRequests(args, options)
			if err != nil {
				return fmt.Errorf("Error creating edges request: %s", err)
			}

			// The gRPC client is concurrency-safe, so we can reuse it in all the following goroutines
			// https://github.com/grpc/grpc-go/issues/682
			client := api.CheckClientOrExit(healthcheck.Options{
				ControlPlaneNamespace: controlPlaneNamespace,
				KubeConfig:            kubeconfigPath,
				Impersonate:           impersonate,
				ImpersonateGroup:      impersonateGroup,
				KubeContext:           kubeContext,
				APIAddr:               apiAddr,
			})

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
					fmt.Fprint(os.Stderr, res.err.Error())
					os.Exit(1)
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
	cmd.PersistentFlags().StringVarP(&options.outputFormat, "output", "o", options.outputFormat, "Output format; one of: \"table\" or \"json\" or \"wide\"")
	cmd.PersistentFlags().BoolVarP(&options.allNamespaces, "all-namespaces", "A", options.allNamespaces, "If present, returns edges across all namespaces, ignoring the \"--namespace\" flag")
	return cmd
}

// validateEdgesRequestInputs ensures that the resource type and output format are both supported
// by the edges command, since the edges command does not support all k8s resource types.
func validateEdgesRequestInputs(targets []*pb.Resource, options *edgesOptions) error {
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
	case tableOutput, jsonOutput, wideOutput:
		return nil
	default:
		return fmt.Errorf("--output supports %s, %s and %s", tableOutput, jsonOutput, wideOutput)
	}
}

func buildEdgesRequests(resources []string, options *edgesOptions) ([]*pb.EdgesRequest, error) {
	targets, err := coreUtil.BuildResources(options.namespace, resources)

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
			ResourceType:  target.Type,
			Namespace:     options.namespace,
			AllNamespaces: options.allNamespaces,
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
	src          string
	srcNamespace string
	dst          string
	dstNamespace string
	client       string
	server       string
	msg          string
}

const (
	srcHeader          = "SRC"
	dstHeader          = "DST"
	srcNamespaceHeader = "SRC_NS"
	dstNamespaceHeader = "DST_NS"
	clientHeader       = "CLIENT_ID"
	serverHeader       = "SERVER_ID"
	msgHeader          = "SECURED"
)

func writeEdgesToBuffer(rows []*pb.Edge, w *tabwriter.Writer, options *edgesOptions) {
	maxSrcLength := len(srcHeader)
	maxDstLength := len(dstHeader)
	maxSrcNamespaceLength := len(srcNamespaceHeader)
	maxDstNamespaceLength := len(dstNamespaceHeader)
	maxClientLength := len(clientHeader)
	maxServerLength := len(serverHeader)
	maxMsgLength := len(msgHeader)

	edgeRows := []edgeRow{}
	if len(rows) != 0 {
		for _, r := range rows {
			clientID := r.ClientId
			serverID := r.ServerId
			msg := r.NoIdentityMsg
			if msg == "" && options.outputFormat != jsonOutput {
				msg = okStatus
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
				client:       clientID,
				server:       serverID,
				msg:          msg,
				src:          r.Src.Name,
				srcNamespace: r.Src.Namespace,
				dst:          r.Dst.Name,
				dstNamespace: r.Dst.Namespace,
			}

			edgeRows = append(edgeRows, row)

			if len(r.Src.Name) > maxSrcLength {
				maxSrcLength = len(r.Src.Name)
			}
			if len(r.Src.Namespace) > maxSrcNamespaceLength {
				maxSrcNamespaceLength = len(r.Src.Namespace)
			}
			if len(r.Dst.Name) > maxDstLength {
				maxDstLength = len(r.Dst.Name)
			}
			if len(r.Dst.Namespace) > maxDstNamespaceLength {
				maxDstNamespaceLength = len(r.Dst.Namespace)
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

	// ordering the rows first by SRC/DST namespace, then by SRC/DST resource
	sort.Slice(edgeRows, func(i, j int) bool {
		keyI := edgeRows[i].srcNamespace + edgeRows[i].dstNamespace + edgeRows[i].src + edgeRows[i].dst
		keyJ := edgeRows[j].srcNamespace + edgeRows[j].dstNamespace + edgeRows[j].src + edgeRows[j].dst
		return keyI < keyJ
	})

	switch options.outputFormat {
	case tableOutput, wideOutput:
		if len(edgeRows) == 0 {
			fmt.Fprintln(os.Stderr, "No edges found.")
			os.Exit(0)
		}
		printEdgeTable(edgeRows, w, maxSrcLength, maxSrcNamespaceLength, maxDstLength, maxDstNamespaceLength, maxClientLength, maxServerLength, maxMsgLength, options.outputFormat)
	case jsonOutput:
		printEdgesJSON(edgeRows, w)
	}
}

func printEdgeTable(edgeRows []edgeRow, w *tabwriter.Writer, maxSrcLength, maxSrcNamespaceLength, maxDstLength, maxDstNamespaceLength, maxClientLength, maxServerLength, maxMsgLength int, outputFormat string) {
	srcTemplate := fmt.Sprintf("%%-%ds", maxSrcLength)
	dstTemplate := fmt.Sprintf("%%-%ds", maxDstLength)
	srcNamespaceTemplate := fmt.Sprintf("%%-%ds", maxSrcNamespaceLength)
	dstNamespaceTemplate := fmt.Sprintf("%%-%ds", maxDstNamespaceLength)
	msgTemplate := fmt.Sprintf("%%-%ds", maxMsgLength)
	clientTemplate := fmt.Sprintf("%%-%ds", maxClientLength)
	serverTemplate := fmt.Sprintf("%%-%ds", maxServerLength)

	headers := []string{
		fmt.Sprintf(srcTemplate, srcHeader),
		fmt.Sprintf(dstTemplate, dstHeader),
		fmt.Sprintf(srcNamespaceTemplate, srcNamespaceHeader),
		fmt.Sprintf(dstNamespaceTemplate, dstNamespaceHeader),
	}

	if outputFormat == wideOutput {
		headers = append(headers, fmt.Sprintf(clientTemplate, clientHeader), fmt.Sprintf(serverTemplate, serverHeader))
	}

	headers = append(headers, fmt.Sprintf(msgTemplate, msgHeader)+"\t")

	fmt.Fprintln(w, strings.Join(headers, "\t"))

	for _, row := range edgeRows {
		values := []interface{}{
			row.src,
			row.dst,
			row.srcNamespace,
			row.dstNamespace,
		}
		templateString := fmt.Sprintf("%s\t%s\t%s\t%s\t", srcTemplate, dstTemplate, srcNamespaceTemplate, dstNamespaceTemplate)

		if outputFormat == wideOutput {
			templateString += fmt.Sprintf("%s\t%s\t", clientTemplate, serverTemplate)
			values = append(values, row.client, row.server)
		}

		templateString += fmt.Sprintf("%s\t\n", msgTemplate)
		values = append(values, row.msg)

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
	Src          string `json:"src"`
	SrcNamespace string `json:"src_namespace"`
	Dst          string `json:"dst"`
	DstNamespace string `json:"dst_namespace"`
	Client       string `json:"client_id"`
	Server       string `json:"server_id"`
	Msg          string `json:"no_tls_reason"`
}

func printEdgesJSON(edgeRows []edgeRow, w *tabwriter.Writer) {
	// avoid nil initialization so that if there are not stats it gets marshalled as an empty array vs null
	entries := []*edgesJSONStats{}

	for _, row := range edgeRows {
		entry := &edgesJSONStats{
			Src:          row.src,
			SrcNamespace: row.srcNamespace,
			Dst:          row.dst,
			DstNamespace: row.dstNamespace,
			Client:       row.client,
			Server:       row.server,
			Msg:          row.msg}
		entries = append(entries, entry)
	}

	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshalling JSON: %s\n", err)
		return
	}
	fmt.Fprintf(w, "%s\n", b)
}
