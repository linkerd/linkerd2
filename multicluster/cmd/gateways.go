package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sync/atomic"
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
		wait        time.Duration
	}

	gatewayMetrics struct {
		clusterName string
		metrics     []byte
		err         error
	}

	gatewayStatus struct {
		clusterName      string
		alive            bool
		numberOfServices int
		latency          uint64
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
			selector := fmt.Sprintf("component=%s", "linkerd-service-mirror")
			if opts.clusterName != "" {
				selector = fmt.Sprintf("%s,mirror.linkerd.io/cluster-name=%s", selector, opts.clusterName)
			}
			pods, err := k8sAPI.CoreV1().Pods(multiclusterNs.Name).List(cmd.Context(), metav1.ListOptions{LabelSelector: selector})
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to list pods in namespace %s: %s", multiclusterNs.Name, err)
				os.Exit(1)
			}

			var statuses []gatewayStatus
			gatewayMetrics := getGatewayMetrics(k8sAPI, pods.Items, opts.wait)
			for _, gateway := range gatewayMetrics {
				if gateway.err != nil {
					fmt.Fprintf(os.Stderr, "Failed to get gateway status for %s: %s\n", gateway.clusterName, gateway.err)
					continue
				}
				gatewayStatus := gatewayStatus{
					clusterName: gateway.clusterName,
				}

				// Parse the gateway metrics so that we can extract liveness
				// and latency information.
				var metricsParser expfmt.TextParser
				parsedMetrics, err := metricsParser.TextToMetricFamilies(bytes.NewReader(gateway.metrics))
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to parse metrics for %s: %s\n", gateway.clusterName, err)
					continue
				}

				// Check if the gateway is alive by using the gateway_alive
				// metric and ensuring the label matches the target cluster.
				for _, metrics := range parsedMetrics["gateway_alive"].GetMetric() {
					if !isTargetClusterMetric(metrics, gateway.clusterName) {
						continue
					}
					if metrics.GetGauge().GetValue() == 1 {
						gatewayStatus.alive = true
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
				gatewayStatus.numberOfServices = len(services.Items)

				// Check the last observed latency by using the
				// gateway_latency metric and ensuring the label the target
				// cluster.
				for _, metrics := range parsedMetrics["gateway_latency"].GetMetric() {
					if !isTargetClusterMetric(metrics, gateway.clusterName) {
						continue
					}
					gatewayStatus.latency = uint64(metrics.GetGauge().GetValue())
					break
				}

				statuses = append(statuses, gatewayStatus)
			}
			renderGateways(statuses, stdout)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", "", "the name of the target cluster")
	cmd.Flags().DurationVarP(&opts.wait, "wait", "w", opts.wait, "time allowed to fetch diagnostics")

	return cmd
}

func getGatewayMetrics(k8sAPI *k8s.KubernetesAPI, pods []corev1.Pod, wait time.Duration) []gatewayMetrics {
	var metrics []gatewayMetrics
	metricsChan := make(chan gatewayMetrics)
	var activeRoutines int32
	for _, pod := range pods {
		atomic.AddInt32(&activeRoutines, 1)
		go func(p corev1.Pod) {
			defer atomic.AddInt32(&activeRoutines, -1)
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
	timeout := time.NewTimer(wait)
	defer timeout.Stop()
wait:
	for {
		select {
		case metric := <-metricsChan:
			metrics = append(metrics, metric)
		case <-timeout.C:
			break wait
		}
		if atomic.LoadInt32(&activeRoutines) == 0 {
			break
		}
	}
	return metrics
}

func getServiceMirrorContainer(pod corev1.Pod) (corev1.Container, error) {
	if pod.Status.Phase != corev1.PodRunning {
		return corev1.Container{}, fmt.Errorf("pod not running: %s", pod.GetName())
	}
	for _, c := range pod.Spec.Containers {
		if c.Name == "service-mirror" {
			return c, nil
		}
	}
	return corev1.Container{}, fmt.Errorf("pod %s did not have 'service-mirror' container", pod.GetName())
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
		if status.alive {
			return value
		}
		return "-"
	}
	alive := "False"
	if status.alive {
		alive = "True"
	}
	return []string{
		status.clusterName,
		alive,
		fmt.Sprint(status.numberOfServices),
		valueOrPlaceholder(fmt.Sprintf("%dms", status.latency)),
	}

}
