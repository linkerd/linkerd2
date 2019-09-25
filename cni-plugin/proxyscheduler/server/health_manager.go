package server

import (
	"fmt"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"time"
)

type HealthMonitor struct {
	runtimeService *CRIRuntime
	kubernetes *KubernetesClient
}

func NewHealthMonitor(runtimeService *CRIRuntime, kubernetes *KubernetesClient) *HealthMonitor {
	return &HealthMonitor{runtimeService:runtimeService, kubernetes:kubernetes}
}

func (p *HealthMonitor) StartHealthMonitor() {
	syncChan := time.NewTicker(5 * time.Second) //TODO: Make configurable
	go p.runMonitorLoop(syncChan.C)
}


func (p *HealthMonitor) runMonitorLoop(syncChan <-chan time.Time) {
	for {
		select {
		case <-syncChan:
			err := p.restartSidecarsIfNeeded()
			if err != nil {
				logrus.Warningf("Could not sync pods: %v", err)
			}
		}
	}
}

func (p *HealthMonitor) restartSidecarsIfNeeded() error {
	logrus.Debug("healthmanager: Checking for failed sidecars")
	podSandboxes, err := p.runtimeService.ListPodSandboxes()
	if err != nil {
		return err
	}

	for _, podSandbox := range podSandboxes {
		podName := podSandbox.Labels["io.kubernetes.pod.name"]
		podNamespace := podSandbox.Labels["io.kubernetes.pod.namespace"]

		logEntry := logrus.WithFields(logrus.Fields{
			"Pod":         podName,
			"Namespace":   podNamespace,
		})

		//TODO: Maybe introduce some kind of a cache to avoid hittin the k8s api too oftenly...
		pod, err := p.kubernetes.getPod(podName, podNamespace)
		if err != nil {
			if errors.IsNotFound(err) {
				continue // pod has been deleted
			}
			return fmt.Errorf("error getting pod %s/%s: %s", podNamespace, podName, err)
		}

		if pod.Annotations != nil && pod.Annotations[schedulerManagedPodAnnotation] == trueValue {
			// so this pod is managed by the scheduler
			// therefore we need to check whether the proxy is healthy and running
			cniNs := pod.Annotations[schedulerManagedPodNsAnnotation] // this should really always be present
			proxyHealthy, err := IsProxyHealthy(podName,  podNamespace, pod.Status.PodIP, cniNs)
			if err != nil {
				logEntry.Errorf("healthmanager: Cannot collect proxy health data for pod %s.", pod.UID)
				continue //TODO: Really wondering what to do here....

			}
			if !proxyHealthy {
				logEntry.Debugf("healthmanager: Proxy for pod %s is not healthy.", pod.UID)
				//TODO: Restart logic
			}
			logEntry.Debugf("healthmanager: Proxy for pod %s is healthy.", pod.UID)
		}
	}
	return nil
}

func IsProxyReady(podName, podNamespace, podIP, netNS string) (bool, error) {
	//TODO: check if proxy is ready for real
	return true, nil
}

func IsProxyHealthy(podName, podNamespace, podIP, netNS string) (bool, error) {
	//TODO: check if proxy is healthy for real
	return true, nil
}

func (p *HealthMonitor) StartMonitoringProxyHealth(podNamespace, podName,  cniNs string) error {
	logEntry := logrus.WithFields(logrus.Fields{
		"Pod":         podName,
		"Namespace":   podNamespace,
	})

	logEntry.Infof("healthmanager: Adding health monitoring annotations %s and %s", schedulerManagedPodAnnotation,schedulerManagedPodNsAnnotation)
	return p.kubernetes.updatePodWithRetries(podNamespace, podName, func(pod *v1.Pod) {
		if pod.Annotations == nil {
			pod.Annotations = make(map[string]string)
		}
		pod.Annotations[schedulerManagedPodAnnotation] = trueValue
		pod.Annotations[schedulerManagedPodNsAnnotation] = cniNs
	})
}
