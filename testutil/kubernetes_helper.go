package testutil

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	coreV1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	// Loads the GCP auth plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

// KubernetesHelper provides Kubernetes-related test helpers. It connects to the
// Kubernetes API using the environment's configured kubeconfig file.
type KubernetesHelper struct {
	clientset *kubernetes.Clientset
	retryFor  func(time.Duration, func() error) error
}

// NewKubernetesHelper creates a new instance of KubernetesHelper.
func NewKubernetesHelper(retryFor func(time.Duration, func() error) error) (*KubernetesHelper, error) {
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
		retryFor:  retryFor,
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
	deploys, err := h.clientset.AppsV1beta2().Deployments(namespace).List(metav1.ListOptions{})
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
	return h.retryFor(30*time.Second, func() error {
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
	})
}

// CheckPods checks that a deployment in a namespace contains the expected
// number of pods in the Running state.
func (h *KubernetesHelper) CheckPods(namespace string, deploymentName string, replicas int) error {
	return h.retryFor(3*time.Minute, func() error {
		pods, err := h.clientset.CoreV1().Pods(namespace).List(metav1.ListOptions{})
		if err != nil {
			return err
		}

		var deploymentReplicas int
		for _, pod := range pods.Items {
			if strings.HasPrefix(pod.Name, deploymentName) {
				deploymentReplicas++
				if pod.Status.Phase != "Running" {
					return fmt.Errorf("Pod [%s] in namespace [%s] is not running",
						pod.Name, pod.Namespace)
				}
				for _, container := range pod.Status.ContainerStatuses {
					if !container.Ready {
						return fmt.Errorf("Container [%s] in pod [%s] in namespace [%s] is not running",
							container.Name, pod.Name, pod.Namespace)
					}
				}
			}
		}

		if deploymentReplicas != replicas {
			return fmt.Errorf("Expected deployment [%s] in namespace [%s] to have [%d] running pods, but found [%d]",
				deploymentName, namespace, replicas, deploymentReplicas)
		}

		return nil
	})
}

// CheckService checks that a service exists in a namespace.
func (h *KubernetesHelper) CheckService(namespace string, serviceName string) error {
	return h.retryFor(10*time.Second, func() error {
		_, err := h.clientset.CoreV1().Services(namespace).Get(serviceName, metav1.GetOptions{})
		return err
	})
}

// GetPodsForDeployment returns all pods for the given deployment
func (h *KubernetesHelper) GetPodsForDeployment(namespace string, deploymentName string) ([]string, error) {
	deploy, err := h.clientset.AppsV1beta2().Deployments(namespace).Get(deploymentName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	podList, err := h.clientset.CoreV1().Pods(namespace).List(metav1.ListOptions{
		LabelSelector: labels.Set(deploy.Spec.Selector.MatchLabels).AsSelector().String(),
	})
	if err != nil {
		return nil, err
	}

	pods := make([]string, 0)
	for _, pod := range podList.Items {
		pods = append(pods, pod.Name)
	}

	return pods, nil
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

// URLFor creates a kubernetes port-forward, runs it, and returns the URL that
// tests can use for access to the given deployment. Note that the port-forward
// remains running for the duration of the test.
func (h *KubernetesHelper) URLFor(namespace, deployName string, remotePort int) (string, error) {
	pf, err := k8s.NewPortForward("", "", namespace, deployName, 0, remotePort)
	if err != nil {
		return "", err
	}

	go pf.Run()
	<-pf.Ready()

	return pf.URLFor(""), nil
}
