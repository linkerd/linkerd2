package testutil

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	coreV1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	// Loads the GCP auth plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

// KubernetesHelper provides Kubernetes-related test helpers. It connects to the
// Kubernetes API using the environment's configured kubeconfig file.
type KubernetesHelper struct {
	clientset *kubernetes.Clientset
}

// NewKubernetesHelper creates a new instance of KubernetesHelper.
func NewKubernetesHelper() (*KubernetesHelper, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)
	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &KubernetesHelper{
		clientset: clientset,
	}, nil
}

// CheckIfNamespaceExists checks if a namespace exists.
func (h *KubernetesHelper) CheckIfNamespaceExists(namespace string) error {
	_, err := h.clientset.CoreV1().Namespaces().Get(namespace, metav1.GetOptions{})
	return err
}

// CreateNamespaceIfNotExists creates a namespace if it does not already exist.
func (h *KubernetesHelper) CreateNamespaceIfNotExists(namespace string) error {
	err := h.CheckIfNamespaceExists(namespace)

	if err != nil {
		ns := &coreV1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		_, err = h.clientset.CoreV1().Namespaces().Create(ns)

		if err != nil {
			return err
		}
	}

	return nil
}

// KubectlApply applies a given configuration string in a namespace. If the
// namespace does not exist, it creates it first. If no namespace is provided,
// it uses the default namespace.
func (h *KubernetesHelper) KubectlApply(stdin string, namespace string) (string, error) {
	if namespace == "" {
		namespace = "default"
	}

	err := h.CreateNamespaceIfNotExists(namespace)
	if err != nil {
		return "", err
	}

	cmd := exec.Command("kubectl", "apply", "-f", "-", "--namespace", namespace)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// getDeployments gets all deployments with a count of their ready replicas in
// the specified namespace.
func (h *KubernetesHelper) getDeployments(namespace string) (map[string]int, error) {
	deploys, err := h.clientset.AppsV1().Deployments(namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	deployments := map[string]int{}
	for _, deploy := range deploys.Items {
		deployments[deploy.GetName()] = int(deploy.Status.ReadyReplicas)
	}
	return deployments, nil
}

// CheckDeployment checks that a deployment in a namespace contains the expected
// number of replicas.
func (h *KubernetesHelper) CheckDeployment(namespace string, deploymentName string, replicas int) error {
	deploys, err := h.getDeployments(namespace)
	if err != nil {
		return err
	}

	count, ok := deploys[deploymentName]
	if !ok {
		return fmt.Errorf("Deployment [%s] in namespace [%s] not found",
			deploymentName, namespace)
	}

	if count != replicas {
		return fmt.Errorf("Expected deployment [%s] in namespace [%s] to have [%d] replicas, but found [%d]",
			deploymentName, namespace, replicas, count)
	}

	return nil
}

// getPods gets all pods with their pod status in the specified namespace.
func (h *KubernetesHelper) getPods(namespace string) (map[string]coreV1.PodPhase, error) {
	pods, err := h.clientset.CoreV1().Pods(namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	podData := make(map[string]coreV1.PodPhase)
	for _, pod := range pods.Items {
		podData[pod.GetName()] = pod.Status.Phase
	}
	return podData, nil
}

// CheckPods checks that a deployment in a namespace contains the expected
// number of pods in the Running state.
func (h *KubernetesHelper) CheckPods(namespace string, deploymentName string, replicas int) error {
	podData, err := h.getPods(namespace)
	if err != nil {
		return err
	}

	var runningPods []string
	for name, status := range podData {
		if strings.Contains(name, deploymentName) {
			if status == "Running" {
				runningPods = append(runningPods, name)
			}
		}
	}

	if len(runningPods) != replicas {
		return fmt.Errorf("Expected deployment [%s] in namespace [%s] to have [%d] running pods, but found [%d]",
			deploymentName, namespace, replicas, len(runningPods))
	}

	return nil
}

// CheckService checks that a service exists in a namespace.
func (h *KubernetesHelper) CheckService(namespace string, serviceName string) error {
	_, err := h.clientset.CoreV1().Services(namespace).Get(serviceName, metav1.GetOptions{})
	return err
}

// GetJobStatus gets the status of a job running in a namespace. If the job does
// not exist it return an error.
func (h *KubernetesHelper) GetJobStatus(namespace, jobName string) (string, error) {
	job, err := h.clientset.BatchV1().Jobs(namespace).Get(jobName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	status := job.Status
	if status.CompletionTime != nil {
		if status.Failed > 0 {
			return "Failed", nil
		}
		return "Completed", nil
	}
	return "Running", nil
}

// ParseNamespacedResource extracts a namespace and resource name from a string
// that's in the format namespace/resource. If the strings is in a different
// format it returns an error.
func (h *KubernetesHelper) ParseNamespacedResource(resource string) (string, string, error) {
	r := regexp.MustCompile("^(.+)\\/(.+)$")
	matches := r.FindAllStringSubmatch(resource, 2)
	if len(matches) == 0 {
		return "", "", fmt.Errorf("string [%s] didn't contain expected format for namespace/resource, extracted: %v", resource, matches)
	}
	return matches[0][1], matches[0][2], nil
}
