package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/linkerd/linkerd2/cli/table"
	"github.com/linkerd/linkerd2/pkg/k8s"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type (
	gatewaysOptions struct {
		clusterName string
		output      string
		wait        time.Duration
	}

	gatewayMetrics struct {
		clusterName string
		metrics     []byte
		err         error
	}

	gatewayStatus struct {
		ClusterName      string `json:"clusterName"`
		Alive            bool   `json:"alive"`
		NumberOfServices int    `json:"numberOfServices"`
		Latency          uint64 `json:"latency"`
	}
)

func newGatewaysOptions() *gatewaysOptions {
	return &gatewaysOptions{
		wait: 30 * time.Second,
	}
}

func newGatewaysCommand() *cobra.Command {

	opts := newGatewaysOptions()

	cmd := &cobra.Command{
		Use:   "gateways",
		Short: "Display stats information about the gateways in target clusters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			// Get all the service mirror components in the
			// linkerd-multicluster namespace which we'll collect gateway
			// metrics from.
			multiclusterNs, err := k8sAPI.GetNamespaceWithExtensionLabel(cmd.Context(), MulticlusterExtensionName)
			if err != nil {
				return fmt.Errorf("make sure the linkerd-multicluster extension is installed, using 'linkerd multicluster install' (%w)", err)
			}
			selector := "component in (linkerd-service-mirror, controller)"
			if opts.clusterName != "" {
				selector = fmt.Sprintf("%s,mirror.linkerd.io/cluster-name=%s", selector, opts.clusterName)
			}
			pods, err := k8sAPI.CoreV1().Pods(multiclusterNs.Name).List(cmd.Context(), metav1.ListOptions{LabelSelector: selector})
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to list pods in namespace %s: %s", multiclusterNs.Name, err)
				os.Exit(1)
			}

			leases, err := k8sAPI.CoordinationV1().Leases(multiclusterNs.Name).List(cmd.Context(), metav1.ListOptions{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to list pods in namespace %s: %s", multiclusterNs.Name, err)
				os.Exit(1)
			}
			// Build a simple lookup table to retrieve Lease object claimants.
			// Metrics should only be pulled from claimants as they are the ones
			// running probes.
			leaders := make(map[string]struct{})
			for _, lease := range leases.Items {
				// If the Lease is not used by the service-mirror, or if it does
				// not have a claimant, then ignore it
				if !strings.Contains(lease.Name, "service-mirror-write") || lease.Spec.HolderIdentity == nil {
					continue
				}

				leaders[*lease.Spec.HolderIdentity] = struct{}{}
			}

			var statuses []gatewayStatus
			gatewayMetrics := getGatewayMetrics(k8sAPI, pods.Items, leaders, opts.wait)
			for _, gateway := range gatewayMetrics {
				if gateway.err != nil {
					fmt.Fprintf(os.Stderr, "Failed to get gateway status for %s: %s\n", gateway.clusterName, gateway.err)
					continue
				}
				gatewayStatus := gatewayStatus{
					ClusterName: gateway.clusterName,
				}

				// Parse the gateway metrics so that we can extract liveness
				// and latency information.
				var metricsParser expfmt.TextParser
				parsedMetrics, err := metricsParser.TextToMetricFamilies(bytes.NewReader(gateway.metrics))
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to parse metrics for %s: %s\n", gateway.clusterName, err)
					continue
				}

				skipGatewayMetrics := false
				for _, metrics := range parsedMetrics["gateway_enabled"].GetMetric() {
					if !isTargetClusterMetric(metrics, gateway.clusterName) {
						continue
					}

					if metrics.GetGauge().GetValue() != 1 {
						skipGatewayMetrics = true
						break
					}
				}

				if skipGatewayMetrics {
					continue
				}

				// Check if the gateway is alive by using the gateway_alive
				// metric and ensuring the label matches the target cluster.
				for _, metrics := range parsedMetrics["gateway_alive"].GetMetric() {
					if !isTargetClusterMetric(metrics, gateway.clusterName) {
						continue
					}
					if metrics.GetGauge().GetValue() == 1 {
						gatewayStatus.Alive = true
						break
					}
				}

				// Search the local cluster for mirror services that are
				// mirrored from the target cluster.
				selector := fmt.Sprintf("%s=%s,%s=%s",
					k8s.MirroredResourceLabel, "true",
					k8s.RemoteClusterNameLabel, gateway.clusterName,
				)
				services, err := k8sAPI.CoreV1().Services(corev1.NamespaceAll).List(context.Background(), metav1.ListOptions{LabelSelector: selector})
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to list services for %s: %s\n", gateway.clusterName, err)
					continue
				}
				gatewayStatus.NumberOfServices = len(services.Items)

				// Check the last observed latency by using the
				// gateway_latency metric and ensuring the label the target
				// cluster.
				for _, metrics := range parsedMetrics["gateway_latency"].GetMetric() {
					if !isTargetClusterMetric(metrics, gateway.clusterName) {
						continue
					}
					gatewayStatus.Latency = uint64(metrics.GetGauge().GetValue())
					break
				}

				statuses = append(statuses, gatewayStatus)
			}

			switch opts.output {
			case "json":
				out, err := json.MarshalIndent(statuses, "", "  ")
				if err != nil {
					fmt.Fprint(os.Stderr, err)
					os.Exit(1)
				}
				fmt.Printf("%s\n", out)
			default:
				renderGateways(statuses, stdout)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", "", "the name of the target cluster")
	cmd.Flags().DurationVarP(&opts.wait, "wait", "w", opts.wait, "time allowed to fetch diagnostics")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "used to print output in different format")

	return cmd
}

func getGatewayMetrics(k8sAPI *k8s.KubernetesAPI, pods []corev1.Pod, leaders map[string]struct{}, wait time.Duration) []gatewayMetrics {
	var metrics []gatewayMetrics
	metricsChan := make(chan gatewayMetrics)
	var wg sync.WaitGroup
	for _, pod := range pods {
		if _, found := leaders[pod.Name]; !found {
			continue
		}

		wg.Add(1)
		go func(p corev1.Pod) {
			defer wg.Done()
			name := p.Labels[k8s.RemoteClusterNameLabel]
			container, err := getServiceMirrorContainer(p)
			if err != nil {
				metricsChan <- gatewayMetrics{
					clusterName: name,
					err:         err,
				}
				return
			}
			metrics, err := k8s.GetContainerMetrics(k8sAPI, p, container, false, k8s.AdminHTTPPortName)
			metricsChan <- gatewayMetrics{
				clusterName: name,
				metrics:     metrics,
				err:         err,
			}
		}(pod)
	}

	go func() {
		wg.Wait()
		close(metricsChan)
	}()

	timeout := time.NewTimer(wait)
	defer timeout.Stop()

wait:
	for {
		select {
		case metric := <-metricsChan:
			if metric.clusterName == "" {
				// channel closed
				break wait
			}
			metrics = append(metrics, metric)
		case <-timeout.C:
			break wait
		}
	}

	return metrics
}

func getServiceMirrorContainer(pod corev1.Pod) (corev1.Container, error) {
	if pod.Status.Phase != corev1.PodRunning {
		return corev1.Container{}, fmt.Errorf("pod not running: %s", pod.GetName())
	}
	for _, c := range pod.Spec.Containers {
		// "controller" is for the service mirror controllers managed by the
		// linkerd-multicluster chart
		if c.Name == "service-mirror" || c.Name == "controller" {
			return c, nil
		}
	}
	return corev1.Container{}, fmt.Errorf("pod %s did not have a 'service-mirror' nor a 'controller' container", pod.GetName())
}

func isTargetClusterMetric(metric *io_prometheus_client.Metric, targetClusterName string) bool {
	for _, label := range metric.GetLabel() {
		if label.GetName() == "target_cluster_name" {
			return label.GetValue() == targetClusterName
		}
	}
	return false
}

func renderGateways(statuses []gatewayStatus, w io.Writer) {
	t := buildGatewaysTable()
	t.Data = []table.Row{}
	for _, status := range statuses {
		status := status
		t.Data = append(t.Data, gatewayStatusToTableRow(status))
	}
	t.Render(w)
}

var (
	clusterNameHeader    = "CLUSTER"
	aliveHeader          = "ALIVE"
	pairedServicesHeader = "NUM_SVC"
	latencyHeader        = "LATENCY"
)

func buildGatewaysTable() table.Table {
	columns := []table.Column{
		{
			Header:    clusterNameHeader,
			Width:     7,
			Flexible:  true,
			LeftAlign: true,
		},
		{
			Header:    aliveHeader,
			Width:     5,
			Flexible:  true,
			LeftAlign: true,
		},
		{
			Header: pairedServicesHeader,
			Width:  9,
		},
		{
			Header: latencyHeader,
			Width:  11,
		},
	}
	t := table.NewTable(columns, []table.Row{})
	t.Sort = []int{0} // sort by cluster name
	return t
}

func gatewayStatusToTableRow(status gatewayStatus) []string {
	valueOrPlaceholder := func(value string) string {
		if status.Alive {
			return value
		}
		return "-"
	}
	alive := "False"
	if status.Alive {
		alive = "True"
	}
	return []string{
		status.ClusterName,
		alive,
		fmt.Sprint(status.NumberOfServices),
		valueOrPlaceholder(fmt.Sprintf("%dms", status.Latency)),
	}

}
