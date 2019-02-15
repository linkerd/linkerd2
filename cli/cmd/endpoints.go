package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/linkerd/linkerd2/controller/api/public"
	"github.com/linkerd/linkerd2/controller/gen/controller/discovery"
	"github.com/linkerd/linkerd2/pkg/addr"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type endpointsOptions struct {
	namespace    string
	outputFormat string
}

var (
	podHeader = "POD"
)

// validate performs all validation on the command-line options.
// It returns the first error encountered, or `nil` if the options are valid.
func (o *endpointsOptions) validate() error {
	switch o.outputFormat {
	case "table", "json", "":
		return nil
	}

	return errors.New("--output currently only supports table and json")
}

func newEndpointsOptions() *endpointsOptions {
	return &endpointsOptions{
		namespace:    "",
		outputFormat: "",
	}
}

func newCmdEndpoints() *cobra.Command {
	options := newEndpointsOptions()

	example := `  # get all endpoints
  linkerd endpoints

  # get endpoints in the emojivoto namespace
  linkerd endpoints -n emojivoto

  # get all endpoints in json
  linkerd endpoints -o json`

	cmd := &cobra.Command{
		Use:     "endpoints [flags]",
		Aliases: []string{"ep"},
		Short:   "Introspect Linkerd's service discovery state",
		Long: `Introspect Linkerd's service discovery state.

This command provides debug information about the internal state of the
control-plane's destination container. Note that this cache of service discovery
information is populated on-demand via linkerd-proxy requests. This command
will return "No endpoints found." until a linkerd-proxy begins routing
requests.`,
		Example: example,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := options.validate()
			if err != nil {
				return err
			}

			endpoints, err := requestEndpointsFromAPI(cliPublicAPIClient())
			if err != nil {
				return fmt.Errorf("Endpoints API error: %s", err)
			}

			output := renderEndpoints(endpoints, options)
			_, err = fmt.Print(output)

			return err
		},
	}

	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of the specified endpoints (default: all namespaces)")
	cmd.PersistentFlags().StringVarP(&options.outputFormat, "output", "o", options.outputFormat, "Output format; currently only \"table\" and \"json\" are supported (default \"table\")")

	return cmd
}

func requestEndpointsFromAPI(client public.APIClient) (*discovery.EndpointsResponse, error) {
	return client.Endpoints(context.Background(), &discovery.EndpointsParams{})
}

func renderEndpoints(endpoints *discovery.EndpointsResponse, options *endpointsOptions) string {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 0, padding, ' ', 0)
	writeEndpointsToBuffer(endpoints, w, options)
	w.Flush()

	return string(buffer.Bytes())
}

type rowEndpoint struct {
	Namespace string `json:"namespace"`
	IP        string `json:"ip"`
	Port      uint32 `json:"port"`
	Pod       string `json:"pod"`
	Version   string `json:"version"`
	Service   string `json:"service"`
}

func writeEndpointsToBuffer(endpoints *discovery.EndpointsResponse, w *tabwriter.Writer, options *endpointsOptions) {
	maxPodLength := len(podHeader)
	maxNamespaceLength := len(namespaceHeader)
	endpointsTables := map[string][]rowEndpoint{}

	for serviceID, servicePort := range endpoints.GetServicePorts() {
		namespace := ""
		parts := strings.SplitN(serviceID, ".", 2)
		if len(parts) == 2 {
			namespace = parts[1]
		}

		if options.namespace != "" && options.namespace != namespace {
			continue
		}

		for port, podAddrs := range servicePort.GetPortEndpoints() {
			for _, podAddr := range podAddrs.GetPodAddresses() {
				pod := podAddr.GetPod()
				name := pod.GetName()
				parts := strings.SplitN(name, "/", 2)
				if len(parts) == 2 {
					name = parts[1]
				}
				row := rowEndpoint{
					Namespace: namespace,
					IP:        addr.PublicIPToString(podAddr.GetAddr().GetIp()),
					Port:      port,
					Pod:       name,
					Version:   pod.GetResourceVersion(),
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
	case "table", "":
		if len(endpointsTables) == 0 {
			fmt.Fprintln(os.Stderr, "No endpoints found.")
			os.Exit(0)
		}
		printEndpointsTables(endpointsTables, w, options, maxPodLength, maxNamespaceLength)
	case "json":
		printEndpointsJSON(endpointsTables, w)
	}
}

func printEndpointsTables(endpointsTables map[string][]rowEndpoint, w *tabwriter.Writer, options *endpointsOptions, maxPodLength int, maxNamespaceLength int) {
	firstTable := true // don't print a newline before the first table

	for _, ns := range sortNamespaceKeys(endpointsTables) {
		if !firstTable {
			fmt.Fprint(w, "\n")
		}
		firstTable = false
		printEndpointsTable(ns, endpointsTables[ns], w, options, maxPodLength, maxNamespaceLength)
	}
}

func printEndpointsTable(namespace string, rows []rowEndpoint, w *tabwriter.Writer, options *endpointsOptions, maxPodLength int, maxNamespaceLength int) {
	headers := make([]string, 0)
	templateString := "%s\t%d\t%s\t%s\t%s\n"

	if options.namespace == "" {
		headers = append(headers, namespaceHeader+strings.Repeat(" ", maxNamespaceLength-len(namespaceHeader)))
		templateString = "%s\t" + templateString
	}

	headers = append(headers, []string{
		"IP",
		"PORT",
		podHeader + strings.Repeat(" ", maxPodLength-len(podHeader)),
		"VERSION",
		"SERVICE",
	}...)
	fmt.Fprintln(w, strings.Join(headers, "\t"))

	for _, row := range rows {
		values := make([]interface{}, 0)
		if options.namespace == "" {
			values = append(values,
				namespace+strings.Repeat(" ", maxNamespaceLength-len(namespace)))
		}

		values = append(values, []interface{}{
			row.IP,
			row.Port,
			row.Pod,
			row.Version,
			row.Service,
		}...)

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
