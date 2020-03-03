package cmd

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/linkerd/linkerd2/controller/api/util"
	"github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/smimetrics"
	"github.com/linkerd/linkerd2/pkg/table"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/api/resource"
)

var allowedKinds = map[string]struct{}{
	k8s.CronJob:               struct{}{},
	k8s.Deployment:            struct{}{},
	k8s.Job:                   struct{}{},
	k8s.Namespace:             struct{}{},
	k8s.Pod:                   struct{}{},
	k8s.ReplicaSet:            struct{}{},
	k8s.ReplicationController: struct{}{},
	k8s.StatefulSet:           struct{}{},
}

type alphaStatOptions struct {
	namespace  string
	toResource string
}

func newCmdAlphaStat() *cobra.Command {
	options := alphaStatOptions{
		namespace: "default",
	}

	statCmd := &cobra.Command{
		Use:   "stat [flags] (RESOURCE)",
		Short: "Display traffic stats about one or many resources",
		Long: `Display traffic stats about one or many resources
		
(RESOURCE) can be a resource kind; one of:
  * cronjobs
  * daemonsets
  * deployments
  * jobs
  * namespace
  * pods
  * replicasets
  * replicationcontrollers
  * statefulsets
or it may be a specific named resource of one of the above kinds.

linkerd alpha stat will return a table of the requested resource or resources
showing the top-line metrics for those resources such as request rate, success
rate, and latency percentiles.  These values are measured on the server-side
unless the --to flag is specified.

The --to flag accepts a resource kind or a specific resource and instead
displays the metrics measured on the client side from the root resource to
the to-resource.  The root resource must be a specific named resource.

Examples:
  # Topline Resource Metrics
  linkerd alpha stat -n emojivoto deploy/web

  # Topline Resource Metrics for a whole Kind
  linkerd alpha stat -n emojivoto deploy

  # Outbound edges
  linkerd alpha stat -n emojivoto deploy/web --to=deploy

  # Outbound to a specific destination
  linkerd alpha stat -n emojivoto deploy/web --to=deploy/emoji`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			target, err := util.BuildResource(options.namespace, args[0])
			if err != nil {
				return err
			}
			if _, ok := allowedKinds[target.GetType()]; !ok {
				return fmt.Errorf("%s is not a supported resource type", target.GetType())
			}
			kind, err := k8s.PluralResourceNameFromFriendlyName(target.GetType())
			if err != nil {
				return err
			}
			name := target.GetName()
			toResource := buildToResource(options.namespace, options.toResource)
			// TODO: Lift this requirement once the API supports it.
			if toResource != nil && toResource.GetType() != target.GetType() {
				return errors.New("the --to resource must have the same kind as the target resource")
			}

			if name != "" {
				if toResource != nil {
					metrics, err := smimetrics.GetTrafficMetricsEdgesList(k8sAPI, target.GetNamespace(), kind, name, nil)
					if err != nil {
						return err
					}
					renderTrafficMetricsEdgesList(metrics, stdout, toResource)
				} else {
					metrics, err := smimetrics.GetTrafficMetrics(k8sAPI, target.GetNamespace(), kind, name, nil)
					if err != nil {
						return err
					}
					renderTrafficMetrics(metrics, stdout)
				}
			} else {
				if toResource != nil {
					return errors.New("the --to flag requires that the target resource name be specified")
				}
				metrics, err := smimetrics.GetTrafficMetricsList(k8sAPI, target.GetNamespace(), kind, nil)
				if err != nil {
					return err
				}
				renderTrafficMetricsList(metrics, stdout)
			}

			return nil
		},
	}

	statCmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of the specified resource")
	statCmd.PersistentFlags().StringVar(&options.toResource, "to", options.toResource, "If present, restricts outbound stats to the specified resource name")

	return statCmd
}

func buildToResource(namespace, to string) *public.Resource {
	toResource, err := util.BuildResource(namespace, to)
	if err != nil {
		log.Debugf("Invalid to resource: %s", err)
		return nil

	}
	log.Debugf("Using to resource: %v", toResource)
	return &toResource
}

func renderTrafficMetrics(metrics *smimetrics.TrafficMetrics, w io.Writer) {
	t := buildTable(false)
	t.Data = []table.Row{metricsToRow(metrics, false)}
	t.Render(w)
}

func renderTrafficMetricsList(metrics *smimetrics.TrafficMetricsList, w io.Writer) {
	t := buildTable(false)
	t.Data = []table.Row{}
	for _, row := range metrics.Items {
		row := row // Copy to satisfy golint.
		t.Data = append(t.Data, metricsToRow(&row, false))
	}
	t.Render(w)
}

func renderTrafficMetricsEdgesList(metrics *smimetrics.TrafficMetricsList, w io.Writer, toResource *public.Resource) {
	outbound := toResource != nil
	t := buildTable(outbound)
	t.Data = []table.Row{}
	for _, row := range metrics.Items {
		row := row // Copy to satisfy golint.
		if row.Edge.Direction != "to" {
			continue
		}
		if toResource != nil && toResource.GetName() != "" &&
			(row.Edge.Resource.Name != toResource.GetName() || row.Edge.Resource.Namespace != toResource.GetNamespace()) {
			log.Debugf("Skipping edge %v", row.Edge.Resource)
			continue
		}
		t.Data = append(t.Data, metricsToRow(&row, outbound))
	}
	t.Render(w)
}

func getNumericMetric(metrics *smimetrics.TrafficMetrics, name string) *resource.Quantity {
	for _, m := range metrics.Metrics {
		if m.Name == name {
			quantity, err := resource.ParseQuantity(m.Value)
			if err != nil {
				return resource.NewQuantity(0, resource.DecimalSI)
			}
			return &quantity
		}
	}
	return resource.NewQuantity(0, resource.DecimalSI)
}

func getNumericMetricWithUnit(metrics *smimetrics.TrafficMetrics, name string) string {
	for _, m := range metrics.Metrics {
		if m.Name == name {
			quantity, err := resource.ParseQuantity(m.Value)
			if err != nil {
				return ""
			}
			value := quantity.Value()
			return fmt.Sprintf("%d%s", value, m.Unit)
		}
	}
	return ""
}

func metricsToRow(metrics *smimetrics.TrafficMetrics, outbound bool) []string {
	success := getNumericMetric(metrics, "success_count").MilliValue()
	failure := getNumericMetric(metrics, "failure_count").MilliValue()
	sr := "-"
	if success+failure > 0 {
		rate := float32(success) / float32(success+failure) * 100
		sr = fmt.Sprintf("%.2f%%", rate)
	}
	rps := "-"
	window, err := time.ParseDuration(metrics.Window)
	if err == nil {
		rate := float64(success+failure) / 1000.0 / window.Seconds()
		rps = fmt.Sprintf("%.1frps", rate)
	}

	var to string
	if outbound {
		to = metrics.Edge.Resource.Name
	}

	return []string{
		metrics.Resource.Name, // Name
		metrics.Resource.Name, // From
		to,                    // To
		sr,
		rps,
		getNumericMetricWithUnit(metrics, "p50_response_latency"),
		getNumericMetricWithUnit(metrics, "p90_response_latency"),
		getNumericMetricWithUnit(metrics, "p99_response_latency"),
	}
}

func buildTable(outbound bool) table.Table {
	columns := []table.Column{
		table.Column{
			Header:    "NAME",
			Width:     4,
			Hide:      outbound,
			Flexible:  true,
			LeftAlign: true,
		},
		table.Column{
			Header:    "FROM",
			Width:     4,
			Hide:      !outbound,
			Flexible:  true,
			LeftAlign: true,
		},
		table.Column{
			Header:    "TO",
			Width:     2,
			Hide:      !outbound,
			Flexible:  true,
			LeftAlign: true,
		},
		table.Column{
			Header: "SUCCESS",
			Width:  7,
		},
		table.Column{
			Header: "RPS",
			Width:  9,
		},
		table.Column{
			Header: "LATENCY_P50",
			Width:  11,
		},
		table.Column{
			Header: "LATENCY_P90",
			Width:  11,
		},
		table.Column{
			Header: "LATENCY_P99",
			Width:  11,
		},
	}
	return table.NewTable(columns, []table.Row{})
}
