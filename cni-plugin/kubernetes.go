// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"strconv"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// newKubeClient is a unit test override variable for interface create.
var newKubeClient = newK8sClient

// getKubePodInfo is a unit test override variable for interface create.
var getKubePodInfo = getK8sPodInfo

// newK8sClient returns a Kubernetes client
func newK8sClient(conf PluginConf, logger *logrus.Entry) (*kubernetes.Clientset, error) {
	// Some config can be passed in a kubeconfig file
	kubeconfig := conf.Kubernetes.Kubeconfig

	// Config can be overridden by config passed in explicitly in the network config.
	configOverrides := &clientcmd.ConfigOverrides{}

	// Use the kubernetes client code to load the kubeconfig file and combine it with the overrides.
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig},
		configOverrides).ClientConfig()
	if err != nil {
		logger.Infof("Failed setting up kubernetes client with kubeconfig %s", kubeconfig)
		return nil, err
	}

	logger.Infof("Set up kubernetes client with kubeconfig %s", kubeconfig)
	logger.Infof("Kubernetes config %v", config)

	// Create the clientset
	return kubernetes.NewForConfig(config)
}

// getK8sPodInfo returns information of a POD
func getK8sPodInfo(client *kubernetes.Clientset, podName, podNamespace string) (containers []string,
	labels map[string]string, annotations map[string]string, ports []string, err error) {
	pod, err := client.CoreV1().Pods(string(podNamespace)).Get(podName, metav1.GetOptions{})
	logrus.Infof("pod info %+v", pod)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	containers = make([]string, len(pod.Spec.Containers))
	for containerIdx, container := range pod.Spec.Containers {
		logrus.WithFields(logrus.Fields{
			"pod":       podName,
			"container": container.Name,
		}).Debug("Inspecting container")
		containers[containerIdx] = container.Name

		if container.Name == "istio-proxy" {
			// don't include ports from istio-proxy in the redirect ports
			continue
		}
		for _, containerPort := range container.Ports {
			logrus.WithFields(logrus.Fields{
				"pod":       podName,
				"container": container.Name,
				"port":      containerPort,
			}).Debug("Added pod port")

			ports = append(ports, strconv.Itoa(int(containerPort.ContainerPort)))
			logrus.WithFields(logrus.Fields{
				"ports":     ports,
				"pod":       podName,
				"container": container.Name,
			}).Debug("port")
		}
	}

	return containers, pod.Labels, pod.Annotations, ports, nil
}
