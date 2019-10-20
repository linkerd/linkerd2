package cmd

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	adminHTTPPortName string = "admin-http"
)

type diagnosticsResult struct {
	pod       string
	container string
	metrics   []byte
	err       error
}
type diagResult []diagnosticsResult

func (s diagResult) Len() int {
	return len(s)
}
func (s diagResult) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s diagResult) Less(i, j int) bool {
	if s[i].pod != s[j].pod {
		return s[i].pod < s[j].pod
	}
	return s[i].container < s[j].container
}

type diagnosticsOptions struct {
	wait time.Duration
}

func newDiagnosticsOptions() *diagnosticsOptions {
	return &diagnosticsOptions{
		wait: 300 * time.Second,
	}
}

func newCmdDiagnostics() *cobra.Command {
	options := newDiagnosticsOptions()

	cmd := &cobra.Command{
		Use:   "diagnostics",
		Short: "Fetch metrics directly from the Linkerd control plane containers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, 0)
			if err != nil {
				return err
			}

			timeoutSeconds := int64(30)
			deployments, err := k8sAPI.AppsV1().Deployments(controlPlaneNamespace).List(metav1.ListOptions{TimeoutSeconds: &timeoutSeconds})
			if err != nil {
				return err
			}

			// ensure we can connect to the public API before fetching the diagnostics.
			checkPublicAPIClientOrRetryOrExit(time.Now().Add(options.wait), true)

			resCount := 0
			resultChan := make(chan diagnosticsResult)
			for _, d := range deployments.Items {
				pods, err := getPodsFor(k8sAPI, controlPlaneNamespace, "deploy/"+d.Name)
				if err != nil {
					fmt.Println(err)
					continue
				}

				for _, pod := range pods {
					containers, err := getNonProxyContainersForPod(pod)
					if err != nil {
						fmt.Println(err)
						continue
					}

					for i := range containers {
						resCount++
						go func(container corev1.Container) {
							bytes, err := getDiagnostics(k8sAPI, pod, container, verbose)

							resultChan <- diagnosticsResult{
								pod:       pod.GetName(),
								container: container.Name,
								metrics:   bytes,
								err:       err,
							}
						}(containers[i])
					}
				}
			}

			var results []diagnosticsResult
			timer := time.NewTimer(options.wait)
			timedOut := false

			for {
				select {
				case result := <-resultChan:
					results = append(results, result)
				case <-timer.C:
					timedOut = true
				}
				if len(results) == resCount || timedOut {
					break
				}
			}

			sort.Sort(diagResult(results))
			for i, result := range results {
				fmt.Printf("#\n# POD %s (%d of %d)\n# CONTAINER %s (%d of %d)\n#\n", result.pod, i+1, len(results), result.container, i+1, len(results))
				if result.err == nil {
					fmt.Printf("%s", result.metrics)
				} else {
					fmt.Printf("# ERROR %s\n", result.err)
				}
			}

			return nil
		},
	}

	cmd.Flags().DurationVarP(&options.wait, "wait", "w", options.wait, "Time allowed to fetch diagnostics")

	return cmd
}

func getDiagnostics(
	k8sAPI *k8s.KubernetesAPI,
	pod corev1.Pod,
	container corev1.Container,
	emitLogs bool,
) ([]byte, error) {
	var port corev1.ContainerPort
	for _, p := range container.Ports {
		if p.Name == adminHTTPPortName {
			port = p
			break
		}
	}
	if port.Name != adminHTTPPortName {
		return nil, fmt.Errorf("no %s port found for container %s", adminHTTPPortName, container.Name)
	}

	portforward, err := k8s.NewPortForward(
		k8sAPI,
		pod.GetNamespace(),
		pod.GetName(),
		"localhost",
		0,
		int(port.ContainerPort),
		emitLogs,
	)
	if err != nil {
		return nil, err
	}

	defer portforward.Stop()
	if err = portforward.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running port-forward: %s", err)
	}

	metricsURL := portforward.URLFor("/metrics")
	resp, err := http.Get(metricsURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return bytes, nil
}

// getNonProxyContainersForPod returns all the containers of a pod except the proxy.
func getNonProxyContainersForPod(
	pod corev1.Pod,
) ([]corev1.Container, error) {
	var containers []corev1.Container

	if pod.Status.Phase != corev1.PodRunning {
		return nil, fmt.Errorf("pod not running: %s", pod.GetName())
	}

	for _, c := range pod.Spec.Containers {
		if c.Name != k8s.ProxyContainerName {
			containers = append(containers, c)
		}
	}

	return containers, nil
}
