package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	destinationPb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	netPb "github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/api/destination"
	"github.com/linkerd/linkerd2/pkg/addr"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

type endpointsOptions struct {
	outputFormat   string
	destinationPod string
	contextToken   string
}

type (
	// map[ServiceID]map[Port][]podData
	endpointsInfo map[string]map[uint32][]podData
	podData       struct {
		name    string
		address string
		ip      string
		weight  uint32
		labels  map[string]string
		http2   *destinationPb.Http2ClientParams
	}
)

const (
	podHeader       = "POD"
	namespaceHeader = "NAMESPACE"
	padding         = 3
)

// validate performs all validation on the command-line options.
// It returns the first error encountered, or `nil` if the options are valid.
func (o *endpointsOptions) validate() error {
	if o.outputFormat == tableOutput || o.outputFormat == jsonOutput {
		return nil
	}

	return fmt.Errorf("--output currently only supports %s and %s", tableOutput, jsonOutput)
}

func newEndpointsOptions() *endpointsOptions {
	return &endpointsOptions{
		outputFormat: tableOutput,
	}
}

func newCmdEndpoints() *cobra.Command {
	options := newEndpointsOptions()

	example := `  # get all endpoints for the authorities emoji-svc.emojivoto.svc.cluster.local:8080 and web-svc.emojivoto.svc.cluster.local:80
  linkerd diagnostics endpoints emoji-svc.emojivoto.svc.cluster.local:8080 web-svc.emojivoto.svc.cluster.local:80

  # get that same information in json format
  linkerd diagnostics endpoints -o json emoji-svc.emojivoto.svc.cluster.local:8080 web-svc.emojivoto.svc.cluster.local:80

  # get the endpoints for authorities in Linkerd's control-plane itself
  linkerd diagnostics endpoints web.linkerd-viz.svc.cluster.local:8084`

	cmd := &cobra.Command{
		Use:     "endpoints [flags] authorities",
		Aliases: []string{"ep"},
		Short:   "Introspect Linkerd's service discovery state",
		Long: `Introspect Linkerd's service discovery state.

This command provides debug information about the internal state of the
control-plane's destination container. It queries the same Destination service
endpoint as the linkerd-proxy's, and returns the addresses associated with that
destination.`,
		Example: example,
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			err := options.validate()
			if err != nil {
				return err
			}

			var client destinationPb.DestinationClient
			var conn *grpc.ClientConn
			if apiAddr != "" {
				client, conn, err = destination.NewClient(apiAddr)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error creating destination client: %s\n", err)
					os.Exit(1)
				}
			} else {
				k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
				if err != nil {
					return err
				}

				client, conn, err = destination.NewExternalClient(cmd.Context(), controlPlaneNamespace, k8sAPI, options.destinationPod)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error creating destination client: %s\n", err)
					os.Exit(1)
				}
			}

			defer conn.Close()

			endpoints, err := requestEndpointsFromAPI(client, options.contextToken, args)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Destination API error: %s\n", err)
				os.Exit(1)
			}

			output := renderEndpoints(endpoints, options)
			_, err = fmt.Print(output)

			return err
		},
	}

	cmd.PersistentFlags().StringVarP(&options.outputFormat, "output", "o", options.outputFormat, fmt.Sprintf("Output format; one of: \"%s\" or \"%s\"", tableOutput, jsonOutput))
	cmd.PersistentFlags().StringVar(&options.destinationPod, "destination-pod", "", "Target a specific destination Pod when there are multiple running")
	cmd.PersistentFlags().StringVar(&options.contextToken, "token", "", "The context token to use when making the request to the destination API")

	pkgcmd.ConfigureOutputFlagCompletion(cmd)

	return cmd
}

func requestEndpointsFromAPI(client destinationPb.DestinationClient, token string, authorities []string) (endpointsInfo, error) {
	info := make(endpointsInfo)
	events := make(chan *destinationPb.Update, 1000)
	errs := make(chan error, 1000)

	for _, authority := range authorities {
		go func(authority string) {
			if len(errs) == 0 {
				dest := &destinationPb.GetDestination{
					Scheme:       "http:",
					Path:         authority,
					ContextToken: token,
				}

				rsp, err := client.Get(context.Background(), dest)
				if err != nil {
					errs <- err
					return
				}

				// Endpoint state may be sent in multiple messages so it's not
				// sufficient to read only the first message. Instead, we
				// continuously read from the stream. This goroutine will never
				// terminate if there are no errors, but this is okay for a
				// short lived CLI command.
				for {
					event, err := rsp.Recv()
					if errors.Is(err, io.EOF) {
						return
					} else if err != nil {
						if grpcError, ok := status.FromError(err); ok {
							err = errors.New(grpcError.Message())
						}
						errs <- err
						return
					}
					events <- event
				}
			}
		}(authority)
	}
	// Wait an amount of time for some endpoint responses to be received.
	timeout := time.NewTimer(5 * time.Second)

	for {
		select {
		case err := <-errs:
			// we only care about the first error
			return nil, err
		case event := <-events:
			addressSet := event.GetAdd()
			labels := addressSet.GetMetricLabels()
			serviceID := labels["service"] + "." + labels["namespace"]
			if _, ok := info[serviceID]; !ok {
				info[serviceID] = make(map[uint32][]podData)
			}

			for _, addr := range addressSet.GetAddrs() {
				tcpAddr := addr.GetAddr()
				port := tcpAddr.GetPort()

				if info[serviceID][port] == nil {
					info[serviceID][port] = make([]podData, 0)
				}

				labels := addr.GetMetricLabels()
				info[serviceID][port] = append(info[serviceID][port], podData{
					name:    labels["pod"],
					address: tcpAddr.String(),
					ip:      getIP(tcpAddr),
					weight:  addr.GetWeight(),
					labels:  addr.GetMetricLabels(),
					http2:   addr.GetHttp2(),
				})
			}
		case <-timeout.C:
			return info, nil
		}
	}
}

func getIP(tcpAddr *netPb.TcpAddress) string {
	ip := addr.FromProxyAPI(tcpAddr.GetIp())
	if ip == nil {
		return ""
	}
	return addr.PublicIPToString(ip)
}

func renderEndpoints(endpoints endpointsInfo, options *endpointsOptions) string {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 0, padding, ' ', 0)
	writeEndpointsToBuffer(endpoints, w, options)
	w.Flush()

	return buffer.String()
}

type rowEndpoint struct {
	Namespace string `json:"namespace"`
	IP        string `json:"ip"`
	Port      uint32 `json:"port"`
	Pod       string `json:"pod"`
	Service   string `json:"service"`
	Weight    uint32 `json:"weight"`

	Http2 *destinationPb.Http2ClientParams `json:"http2,omitempty"`

	Labels map[string]string `json:"labels"`
}

func writeEndpointsToBuffer(endpoints endpointsInfo, w *tabwriter.Writer, options *endpointsOptions) {
	maxPodLength := len(podHeader)
	maxNamespaceLength := len(namespaceHeader)
	endpointsTables := map[string][]rowEndpoint{}

	for serviceID, servicePort := range endpoints {
		namespace := ""
		parts := strings.SplitN(serviceID, ".", 2)
		namespace = parts[1]

		for port, podAddrs := range servicePort {
			for _, pod := range podAddrs {
				name := pod.name
				parts := strings.SplitN(name, "/", 2)
				if len(parts) == 2 {
					name = parts[1]
				}
				row := rowEndpoint{
					Namespace: namespace,
					IP:        pod.ip,
					Port:      port,
					Pod:       name,
					Service:   serviceID,
					Weight:    pod.weight,
					Labels:    pod.labels,
					Http2:     pod.http2,
				}

				endpointsTables[namespace] = append(endpointsTables[namespace], row)

				if len(name) > maxPodLength {
					maxPodLength = len(name)
				}
				if len(namespace) > maxNamespaceLength {
					maxNamespaceLength = len(namespace)
				}
			}

			sort.Slice(endpointsTables[namespace], func(i, j int) bool {
				return endpointsTables[namespace][i].Service < endpointsTables[namespace][j].Service
			})
		}
	}

	switch options.outputFormat {
	case tableOutput:
		if len(endpointsTables) == 0 {
			fmt.Fprintln(os.Stderr, "No endpoints found.")
			os.Exit(0)
		}
		printEndpointsTables(endpointsTables, w, maxPodLength, maxNamespaceLength)
	case jsonOutput:
		printEndpointsJSON(endpointsTables, w)
	}
}

func printEndpointsTables(endpointsTables map[string][]rowEndpoint, w *tabwriter.Writer, maxPodLength int, maxNamespaceLength int) {
	firstTable := true // don't print a newline before the first table

	for _, ns := range sortNamespaceKeys(endpointsTables) {
		if !firstTable {
			fmt.Fprint(w, "\n")
		}
		firstTable = false
		printEndpointsTable(ns, endpointsTables[ns], w, maxPodLength, maxNamespaceLength)
	}
}

func printEndpointsTable(namespace string, rows []rowEndpoint, w *tabwriter.Writer, maxPodLength int, maxNamespaceLength int) {
	headers := make([]string, 0)
	templateString := "%s\t%d\t%s\t%s\n"

	headers = append(headers, namespaceHeader+strings.Repeat(" ", maxNamespaceLength-len(namespaceHeader)))
	templateString = "%s\t" + templateString

	headers = append(headers, []string{
		"IP",
		"PORT",
		podHeader + strings.Repeat(" ", maxPodLength-len(podHeader)),
		"SERVICE",
	}...)
	fmt.Fprintln(w, strings.Join(headers, "\t"))

	for _, row := range rows {
		values := []interface{}{
			namespace + strings.Repeat(" ", maxNamespaceLength-len(namespace)),
			row.IP,
			row.Port,
			row.Pod,
			row.Service,
		}

		fmt.Fprintf(w, templateString, values...)
	}
}

func printEndpointsJSON(endpointsTables map[string][]rowEndpoint, w *tabwriter.Writer) {
	entries := []rowEndpoint{}

	for _, ns := range sortNamespaceKeys(endpointsTables) {
		entries = append(entries, endpointsTables[ns]...)
	}

	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		log.Error(err.Error())
		return
	}
	fmt.Fprintf(w, "%s\n", b)
}

func sortNamespaceKeys(endpointsTables map[string][]rowEndpoint) []string {
	var sortedKeys []string
	for key := range endpointsTables {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)
	return sortedKeys
}
