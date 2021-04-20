package cmd

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"

	destinationPb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	netPb "github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/api/destination"
	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/status"
)

type endpointsOptions struct {
	outputFormat string
}

type (
	// map[ServiceID]map[Port][]podData
	endpointsInfo map[string]map[uint32][]podData
	podData       struct {
		name    string
		address string
		ip      string
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

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			client, conn, err := destination.NewExternalClient(cmd.Context(), controlPlaneNamespace, k8sAPI)
			if err != nil {
				fmt.Fprint(os.Stderr, fmt.Errorf("Error creating destination client: %s", err))
				os.Exit(1)
			}
			defer conn.Close()

			endpoints, err := requestEndpointsFromAPI(client, args)
			if err != nil {
				fmt.Fprint(os.Stderr, fmt.Errorf("Destination API error: %s", err))
				os.Exit(1)
			}

			output := renderEndpoints(endpoints, options)
			_, err = fmt.Print(output)

			return err
		},
	}

	cmd.PersistentFlags().StringVarP(&options.outputFormat, "output", "o", options.outputFormat, fmt.Sprintf("Output format; one of: \"%s\" or \"%s\"", tableOutput, jsonOutput))

	return cmd
}

func requestEndpointsFromAPI(client destinationPb.DestinationClient, authorities []string) (endpointsInfo, error) {
	info := make(endpointsInfo)
	// buffered channels to avoid blocking
	events := make(chan *destinationPb.Update, len(authorities))
	errs := make(chan error, len(authorities))
	var wg sync.WaitGroup

	for _, authority := range authorities {
		wg.Add(1)
		go func(authority string) {
			defer wg.Done()
			if len(errs) == 0 {
				dest := &destinationPb.GetDestination{
					Scheme: "http:",
					Path:   authority,
				}

				rsp, err := client.Get(context.Background(), dest)
				if err != nil {
					errs <- err
					return
				}

				event, err := rsp.Recv()
				if err != nil {
					if grpcError, ok := status.FromError(err); ok {
						err = errors.New(grpcError.Message())
					}
					errs <- err
					return
				}
				events <- event
			}
		}(authority)
	}
	// Block till all goroutines above are done
	wg.Wait()

	for i := 0; i < len(authorities); i++ {
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
				})
			}
		}
	}

	return info, nil
}

func getIP(tcpAddr *netPb.TcpAddress) string {
	ip := tcpAddr.GetIp().GetIpv4()
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, ip)
	return net.IP(b).String()
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
