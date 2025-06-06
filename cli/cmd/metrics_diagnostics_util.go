package cmd

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/common/expfmt"
	corev1 "k8s.io/api/core/v1"
)

// shared between metrics and diagnostics command
type metricsResult struct {
	pod       string
	container string
	metrics   []byte
	err       error
}
type byResult []metricsResult

func (s byResult) Len() int {
	return len(s)
}
func (s byResult) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s byResult) Less(i, j int) bool {
	return s[i].pod < s[j].pod || ((s[i].pod == s[j].pod) && s[i].container < s[j].container)
}

// getAllContainersWithPortSuffix returns all the containers within
// a pod which exposes metrics at a port with the given suffix
func getAllContainersWithPortSuffix(
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
			if p.Name == portName || strings.HasSuffix(p.Name, portName) {
				containers = append(containers, c)
			}
		}
	}
	return containers, nil
}

// getMetrics returns the metrics exposed by all the containers of the passed in list of pods
// which exposes their metrics at portName
func getMetrics(
	k8sAPI *k8s.KubernetesAPI,
	pods []corev1.Pod,
	portName string,
	waitingTime time.Duration,
	emitLogs bool,
) []metricsResult {
	var results []metricsResult

	resultChan := make(chan metricsResult)
	var activeRoutines int32
	for _, pod := range pods {
		atomic.AddInt32(&activeRoutines, 1)
		go func(p corev1.Pod) {
			defer atomic.AddInt32(&activeRoutines, -1)
			containers, err := getAllContainersWithPortSuffix(p, portName)
			if err != nil {
				resultChan <- metricsResult{
					pod: p.GetName(),
					err: err,
				}
				return
			}

			for _, c := range containers {
				bytes, err := k8s.GetContainerMetrics(k8sAPI, p, c, emitLogs, portName)

				resultChan <- metricsResult{
					pod:       p.GetName(),
					container: c.Name,
					metrics:   bytes,
					err:       err,
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

var obfuscationMap = map[string]struct{}{
	"authority":     {},
	"client_id":     {},
	"server_id":     {},
	"target_addr":   {},
	"dst_service":   {},
	"dst_namespace": {},
}

func obfuscateMetrics(metrics []byte) ([]byte, error) {
	reader := bytes.NewReader(metrics)

	var metricsParser expfmt.TextParser

	parsedMetrics, err := metricsParser.TextToMetricFamilies(reader)
	if err != nil {
		return nil, err
	}

	var writer bytes.Buffer
	for _, v := range parsedMetrics {
		for _, m := range v.Metric {
			for _, l := range m.Label {
				if _, ok := obfuscationMap[l.GetName()]; ok {
					obfuscatedValue := obfuscate(l.GetValue())
					l.Value = &obfuscatedValue
				}
			}
		}
		// We'll assume MetricFamilyToText errors are insignificant
		//nolint:errcheck
		expfmt.MetricFamilyToText(&writer, v)
	}

	return writer.Bytes(), nil
}

func obfuscate(s string) string {
	hash := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", hash[:4])
}
