package testutil

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
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
	k8sContext string
	clientset  *kubernetes.Clientset
	retryFor   func(time.Duration, func() error) error
}

// RestartCountError is returned by CheckPods() whenever a pod has restarted exactly one time.
// Consumers should log this type of error instead of failing the test.
// This is to alleviate CI flakiness stemming from a containerd bug.
// See https://github.com/kubernetes/kubernetes/issues/89064
// See https://github.com/containerd/containerd/issues/4068
type RestartCountError struct {
	msg string
}

func (e *RestartCountError) Error() string {
	return e.msg
}

// NewKubernetesHelper creates a new instance of KubernetesHelper.
func NewKubernetesHelper(k8sContext string, retryFor func(time.Duration, func() error) error) (*KubernetesHelper, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: k8sContext}
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
		clientset:  clientset,
		k8sContext: k8sContext,
		retryFor:   retryFor,
	}, nil
}

// CheckIfNamespaceExists checks if a namespace exists.
func (h *KubernetesHelper) CheckIfNamespaceExists(ctx context.Context, namespace string) error {
	_, err := h.clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	return err
}

// GetSecret retrieves a Kubernetes Secret
func (h *KubernetesHelper) GetSecret(ctx context.Context, namespace, name string) (*corev1.Secret, error) {
	return h.clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
}

func (h *KubernetesHelper) createNamespaceIfNotExists(ctx context.Context, namespace string, annotations, labels map[string]string) error {
	err := h.CheckIfNamespaceExists(ctx, namespace)

	if err != nil {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      labels,
				Annotations: annotations,
				Name:        namespace,
			},
		}
		_, err = h.clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})

		if err != nil {
			return err
		}
	}

	return nil
}

// DeleteNamespaceIfExists attempts to delete the given namespace,
// using the K8s API directly
func (h *KubernetesHelper) DeleteNamespaceIfExists(ctx context.Context, namespace string) error {
	err := h.clientset.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})

	if err != nil && !kerrors.IsNotFound(err) {
		return err
	}
	return nil
}

// CreateControlPlaneNamespaceIfNotExists creates linkerd control plane namespace.
func (h *KubernetesHelper) CreateControlPlaneNamespaceIfNotExists(ctx context.Context, namespace string) error {
	labels := map[string]string{"linkerd.io/is-control-plane": "true", "config.linkerd.io/admission-webhooks": "disabled", "linkerd.io/control-plane-ns": namespace}
	annotations := map[string]string{"linkerd.io/inject": "disabled"}
	return h.createNamespaceIfNotExists(ctx, namespace, annotations, labels)
}

// CreateDataPlaneNamespaceIfNotExists creates a dataplane namespace if it does not already exist,
// with a test.linkerd.io/is-test-data-plane label for easier cleanup afterwards
func (h *KubernetesHelper) CreateDataPlaneNamespaceIfNotExists(ctx context.Context, namespace string, annotations map[string]string) error {
	return h.createNamespaceIfNotExists(ctx, namespace, annotations, map[string]string{"test.linkerd.io/is-test-data-plane": "true"})
}

// KubectlApply applies a given configuration string in a namespace. If the
// namespace does not exist, it creates it first. If no namespace is provided,
// it does not specify the `--namespace` flag.
func (h *KubernetesHelper) KubectlApply(stdin string, namespace string) (string, error) {
	args := []string{"apply", "-f", "-"}
	if namespace != "" {
		args = append(args, "--namespace", namespace)
	}

	return h.Kubectl(stdin, args...)
}

// KubectlApplyWithArgs applies a given configuration string with the passed
// flags
func (h *KubernetesHelper) KubectlApplyWithArgs(stdin string, cmdArgs ...string) (string, error) {
	args := []string{"apply"}
	args = append(args, cmdArgs...)
	args = append(args, "-f", "-")
	return h.Kubectl(stdin, args...)
}

// Kubectl executes an arbitrary Kubectl command
func (h *KubernetesHelper) Kubectl(stdin string, arg ...string) (string, error) {
	withContext := append([]string{"--context=" + h.k8sContext}, arg...)
	cmd := exec.Command("kubectl", withContext...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// GetConfigUID returns the uid associated to the linkerd-config ConfigMap resource
// in the given namespace
func (h *KubernetesHelper) GetConfigUID(ctx context.Context, namespace string) (string, error) {
	cm, err := h.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, k8s.ConfigConfigMapName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return string(cm.GetUID()), nil
}

// GetResources returns the resource limits and requests set on a deployment
// of the set name in the given namespace
func (h *KubernetesHelper) GetResources(ctx context.Context, containerName, deploymentName, namespace string) (corev1.ResourceRequirements, error) {
	dep, err := h.clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}

	for _, container := range dep.Spec.Template.Spec.Containers {
		if container.Name == containerName {
			return container.Resources, nil
		}
	}
	return corev1.ResourceRequirements{}, fmt.Errorf("container %s not found in deployment %s in namespace %s", containerName, deploymentName, namespace)
}

// CheckPods checks that a deployment in a namespace contains the expected
// number of pods in the Running state, and that no pods have been restarted.
func (h *KubernetesHelper) CheckPods(ctx context.Context, namespace string, deploymentName string, replicas int) error {
	var checkedPods []corev1.Pod

	err := h.retryFor(6*time.Minute, func() error {
		checkedPods = []corev1.Pod{}
		pods, err := h.GetPodsForDeployment(ctx, namespace, deploymentName)
		if err != nil {
			return err
		}

		var deploymentReplicas int
		for _, pod := range pods {
			checkedPods = append(checkedPods, pod)

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

		if deploymentReplicas != replicas {
			return fmt.Errorf("Expected there to be [%d] pods in deployment [%s] in namespace [%s], but found [%d]",
				replicas, deploymentName, namespace, deploymentReplicas)
		}

		return nil
	})

	if err != nil {
		return err
	}

	for _, pod := range checkedPods {
		for _, status := range append(pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses...) {
			errStr := fmt.Sprintf("Container [%s] in pod [%s] in namespace [%s] has restart count [%d]",
				status.Name, pod.Name, pod.Namespace, status.RestartCount)
			if status.RestartCount == 1 {
				return &RestartCountError{errStr}
			}
			if status.RestartCount > 1 {
				return errors.New(errStr)
			}
		}
	}

	return nil
}

// CheckService checks that a service exists in a namespace.
func (h *KubernetesHelper) CheckService(ctx context.Context, namespace string, serviceName string) error {
	return h.retryFor(10*time.Second, func() error {
		_, err := h.clientset.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
		return err
	})
}

// GetService gets a service that exists in a namespace.
func (h *KubernetesHelper) GetService(ctx context.Context, namespace string, serviceName string) (*corev1.Service, error) {
	service, err := h.clientset.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return service, nil
}

// GetPods returns all pods with the given labels
func (h *KubernetesHelper) GetPods(ctx context.Context, namespace string, podLabels map[string]string) ([]corev1.Pod, error) {
	podList, err := h.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(podLabels).AsSelector().String(),
	})
	if err != nil {
		return nil, err
	}

	return podList.Items, nil
}

// GetPodsForDeployment returns all pods for the given deployment
func (h *KubernetesHelper) GetPodsForDeployment(ctx context.Context, namespace string, deploymentName string) ([]corev1.Pod, error) {
	deploy, err := h.clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return h.GetPods(ctx, namespace, deploy.Spec.Selector.MatchLabels)
}

// GetPodNamesForDeployment returns all pod names for the given deployment
func (h *KubernetesHelper) GetPodNamesForDeployment(ctx context.Context, namespace string, deploymentName string) ([]string, error) {
	podList, err := h.GetPodsForDeployment(ctx, namespace, deploymentName)
	if err != nil {
		return nil, err
	}

	pods := make([]string, 0)
	for _, pod := range podList {
		pods = append(pods, pod.Name)
	}

	return pods, nil
}

// ParseNamespacedResource extracts a namespace and resource name from a string
// that's in the format namespace/resource. If the strings is in a different
// format it returns an error.
func (h *KubernetesHelper) ParseNamespacedResource(resource string) (string, string, error) {
	r := regexp.MustCompile(`^(.+)\/(.+)$`)
	matches := r.FindAllStringSubmatch(resource, 2)
	if len(matches) == 0 {
		return "", "", fmt.Errorf("string [%s] didn't contain expected format for namespace/resource, extracted: %v", resource, matches)
	}
	return matches[0][1], matches[0][2], nil
}

// URLFor creates a kubernetes port-forward, runs it, and returns the URL that
// tests can use for access to the given deployment. Note that the port-forward
// remains running for the duration of the test.
func (h *KubernetesHelper) URLFor(ctx context.Context, namespace, deployName string, remotePort int) (string, error) {
	k8sAPI, err := k8s.NewAPI("", h.k8sContext, "", []string{}, 0)
	if err != nil {
		return "", err
	}

	pf, err := k8s.NewPortForward(ctx, k8sAPI, namespace, deployName, "localhost", 0, remotePort, false)
	if err != nil {
		return "", err
	}

	if err = pf.Init(); err != nil {
		return "", err
	}

	return pf.URLFor(""), nil
}

// WaitRollout blocks until all the deployments in the linkerd namespace have been
// completely rolled out (and their pods are ready)
func (h *KubernetesHelper) WaitRollout(t *testing.T) {
	for deploy, deploySpec := range LinkerdDeployReplicasEdge {
		if deploySpec.Namespace == "linkerd" {
			o, err := h.Kubectl("", "--namespace=linkerd", "rollout", "status", "--timeout=120s", "deploy/"+deploy)
			if err != nil {
				AnnotatedFatalf(t,
					fmt.Sprintf("failed to wait rollout of deploy/%s", deploy),
					"failed to wait for rollout of deploy/%s: %s: %s", deploy, err, o)
			}
		}
	}
}
