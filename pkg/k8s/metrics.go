package k8s

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// AdminHTTPPortName is the name of the port used by the admin http server.
const AdminHTTPPortName string = "admin-http"

// GetContainerMetrics returns the metrics exposed by a container on the passed in portName
func GetContainerMetrics(
	k8sAPI *KubernetesAPI,
	pod corev1.Pod,
	container corev1.Container,
	emitLogs bool,
	portName string,
) ([]byte, error) {
	portForward, err := NewContainerMetricsForward(k8sAPI, pod, container, emitLogs, portName)
	if err != nil {
		return nil, err
	}

	defer portForward.Stop()
	if err = portForward.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running port-forward: %s", err)
		return nil, err
	}

	metricsURL := portForward.URLFor("/metrics")
	return getResponse(metricsURL)
}

// getResponse makes a http Get request to the passed url and returns the response/error
func getResponse(url string) ([]byte, error) {
	// url has been constructed by k8s.newPortForward and is not passed in by
	// the user.
	//nolint:gosec
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// getAllContainersWithPort returns all the containers within
// a pod which exposes metrics at a port with name portName
func GetAllContainersWithPort(
	pod corev1.Pod,
	portName string,
) ([]corev1.Container, error) {
	if pod.Status.Phase != corev1.PodRunning {
		return nil, fmt.Errorf("pod not running: %s", pod.GetName())
	}
	var containers []corev1.Container

	allContainers := append(pod.Spec.InitContainers, pod.Spec.Containers...)
	for _, c := range allContainers {
		for _, p := range c.Ports {
			if p.Name == portName {
				containers = append(containers, c)
			}
		}
	}
	return containers, nil
}

// shared between metrics and diagnostics command
type MetricsResult struct {
	Pod       string
	Container string
	Metrics   []byte
	Err       error
}
type byResult []MetricsResult

func (s byResult) Len() int {
	return len(s)
}
func (s byResult) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s byResult) Less(i, j int) bool {
	return s[i].Pod < s[j].Pod || ((s[i].Pod == s[j].Pod) && s[i].Container < s[j].Container)
}

// getMetrics returns the metrics exposed by all the containers of the passed in list of pods
// which exposes their metrics at portName
func GetMetrics(
	k8sAPI *KubernetesAPI,
	pods []corev1.Pod,
	portName string,
	waitingTime time.Duration,
	emitLogs bool,
) []MetricsResult {
	var results []MetricsResult

	resultChan := make(chan MetricsResult)
	var activeRoutines int32
	for _, pod := range pods {
		atomic.AddInt32(&activeRoutines, 1)
		go func(p corev1.Pod) {
			defer atomic.AddInt32(&activeRoutines, -1)
			containers, err := GetAllContainersWithPort(p, portName)
			if err != nil {
				resultChan <- MetricsResult{
					Pod: p.GetName(),
					Err: err,
				}
				return
			}

			for _, c := range containers {
				bytes, err := GetContainerMetrics(k8sAPI, p, c, emitLogs, portName)

				resultChan <- MetricsResult{
					Pod:       p.GetName(),
					Container: c.Name,
					Metrics:   bytes,
					Err:       err,
				}
			}
		}(pod)
	}

	timeout := time.NewTimer(waitingTime)
	defer timeout.Stop()
wait:
	for {
		select {
		case result := <-resultChan:
			results = append(results, result)
		case <-timeout.C:
			break wait // timed out
		}
		if atomic.LoadInt32(&activeRoutines) == 0 {
			break
		}
	}

	sort.Sort(byResult(results))

	return results
}
