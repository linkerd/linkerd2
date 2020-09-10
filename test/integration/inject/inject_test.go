package inject

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/linkerd/linkerd2/testutil"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(testutil.Run(m, TestHelper))
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestInjectManual(t *testing.T) {
	cmd := []string{"inject",
		"--manual",
		"--linkerd-namespace=fake-ns",
		"--disable-identity",
		"--ignore-cluster",
		"--proxy-version=proxy-version",
		"--proxy-image=proxy-image",
		"--init-image=init-image",
		"testdata/inject_test.yaml",
	}
	out, stderr, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v: %s", stderr, err)
	}

	err = testutil.ValidateInject(out, "injected_default.golden", TestHelper)
	if err != nil {
		testutil.AnnotatedFatalf(t, "received unexpected output", "received unexpected output\n%s", err.Error())
	}
}

func TestInjectManualParams(t *testing.T) {
	// TODO: test config.linkerd.io/proxy-version
	cmd := []string{"inject",
		"--manual",
		"--linkerd-namespace=fake-ns",
		"--disable-identity",
		"--disable-tap",
		"--ignore-cluster",
		"--proxy-version=proxy-version",
		"--proxy-image=proxy-image",
		"--init-image=init-image",
		"--image-pull-policy=Never",
		"--control-port=123",
		"--skip-inbound-ports=234,345",
		"--skip-outbound-ports=456,567",
		"--inbound-port=678",
		"--admin-port=789",
		"--outbound-port=890",
		"--proxy-cpu-request=10m",
		"--proxy-memory-request=10Mi",
		"--proxy-cpu-limit=20m",
		"--proxy-memory-limit=20Mi",
		"--proxy-uid=1337",
		"--proxy-log-level=warn",
		"--enable-external-profiles",
		"testdata/inject_test.yaml",
	}

	out, stderr, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v: %s", stderr, err)
	}

	err = testutil.ValidateInject(out, "injected_params.golden", TestHelper)
	if err != nil {
		testutil.AnnotatedFatalf(t, "received unexpected output", "received unexpected output\n%s", err.Error())
	}
}

func TestInjectAutoNamespaceOverrideAnnotations(t *testing.T) {
	// Check for Namespace level override of proxy Configurations
	injectYAML, err := testutil.ReadFile("testdata/inject_test.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to read inject test file", "failed to read inject test file: %s", err)
	}

	injectNS := "inj-ns-override-test"
	deployName := "inject-test-terminus"
	nsProxyMemReq := "50Mi"
	nsProxyCPUReq := "200m"

	// Namespace level proxy configuration override
	nsAnnotations := map[string]string{
		k8s.ProxyInjectAnnotation:        k8s.ProxyInjectEnabled,
		k8s.ProxyCPURequestAnnotation:    nsProxyCPUReq,
		k8s.ProxyMemoryRequestAnnotation: nsProxyMemReq,
	}

	ns := TestHelper.GetTestNamespace(injectNS)
	err = TestHelper.CreateDataPlaneNamespaceIfNotExists(ns, nsAnnotations)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to create namespace", "failed to create %s namespace: %s", ns, err)
	}

	// patch injectYAML with unique name and pod annotations
	// Pod Level proxy configuration override
	podProxyCPUReq := "600m"
	podAnnotations := map[string]string{
		k8s.ProxyCPURequestAnnotation: podProxyCPUReq,
	}

	patchedYAML, err := testutil.PatchDeploy(injectYAML, deployName, podAnnotations)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to patch inject test YAML",
			"failed to patch inject test YAML in namespace %s for deploy/%s: %s", ns, deployName, err)
	}

	o, err := TestHelper.Kubectl(patchedYAML, "--namespace", ns, "create", "-f", "-")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to create deployment", "failed to create deploy/%s in namespace %s for  %s: %s", deployName, ns, err, o)
	}

	o, err = TestHelper.Kubectl("", "--namespace", ns, "wait", "--for=condition=available", "--timeout=120s", "deploy/"+deployName)
	if err != nil {
		testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to wait for condition=available for deploy/%s in namespace %s", deployName, ns),
			"failed to wait for condition=available for deploy/%s in namespace %s: %s: %s", deployName, ns, err, o)
	}

	pods, err := TestHelper.GetPodsForDeployment(ns, deployName)
	if err != nil {
		testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to get pods for namespace %s", ns),
			"failed to get pods for namespace %s: %s", ns, err)
	}

	containers := pods[0].Spec.Containers
	proxyContainer := testutil.GetProxyContainer(containers)

	// Match the pod configuration with the namespace level overrides
	if proxyContainer.Resources.Requests["memory"] != resource.MustParse(nsProxyMemReq) {
		testutil.Fatalf(t, "proxy memory resource request failed to match with namespace level override")
	}

	// Match with proxy level override
	if proxyContainer.Resources.Requests["cpu"] != resource.MustParse(podProxyCPUReq) {
		testutil.Fatalf(t, "proxy cpu resource request failed to match with pod level override")
	}
}

func TestInjectAutoAnnotationPermutations(t *testing.T) {
	injectYAML, err := testutil.ReadFile("testdata/inject_test.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to read inject test file", "failed to read inject test file: %s", err)
	}

	injectNS := "inject-test"
	deployName := "inject-test-terminus"

	injectAnnotations := []string{"", k8s.ProxyInjectDisabled, k8s.ProxyInjectEnabled}

	// deploy
	for _, nsAnnotation := range injectAnnotations {
		nsPrefix := injectNS
		nsAnnotations := map[string]string{}
		if nsAnnotation != "" {
			nsAnnotations[k8s.ProxyInjectAnnotation] = nsAnnotation
			nsPrefix = fmt.Sprintf("%s-%s", nsPrefix, nsAnnotation)
		}
		ns := TestHelper.GetTestNamespace(nsPrefix)

		err = TestHelper.CreateDataPlaneNamespaceIfNotExists(ns, nsAnnotations)
		if err != nil {
			testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to create %s namespace with annotation %s", ns, nsAnnotation),
				"failed to create %s namespace with annotation %s: %s", ns, nsAnnotation, err)
		}

		for _, podAnnotation := range injectAnnotations {
			// patch injectYAML with unique name and pod annotations
			name := deployName
			podAnnotations := map[string]string{}
			if podAnnotation != "" {
				podAnnotations[k8s.ProxyInjectAnnotation] = podAnnotation
				name = fmt.Sprintf("%s-%s", name, podAnnotation)
			}

			patchedYAML, err := testutil.PatchDeploy(injectYAML, name, podAnnotations)
			if err != nil {
				testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to patch inject test YAML in namespace %s for deploy/%s", ns, name),
					"failed to patch inject test YAML in namespace %s for deploy/%s: %s", ns, name, err)
			}

			o, err := TestHelper.Kubectl(patchedYAML, "--namespace", ns, "create", "-f", "-")
			if err != nil {
				testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to create deploy/%s in namespace %s", name, ns),
					"failed to create deploy/%s in namespace %s for %s: %s", name, ns, err, o)
			}
		}
	}

	containerName := "bb-terminus"

	// check for successful deploy
	for _, nsAnnotation := range injectAnnotations {
		nsPrefix := injectNS
		if nsAnnotation != "" {
			nsPrefix = fmt.Sprintf("%s-%s", nsPrefix, nsAnnotation)
		}
		ns := TestHelper.GetTestNamespace(nsPrefix)

		for _, podAnnotation := range injectAnnotations {
			name := deployName
			if podAnnotation != "" {
				name = fmt.Sprintf("%s-%s", name, podAnnotation)
			}

			o, err := TestHelper.Kubectl("", "--namespace", ns, "wait", "--for=condition=available", "--timeout=120s", "deploy/"+name)
			if err != nil {
				testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to wait for condition=available for deploy/%s in namespace %s", name, ns),
					"failed to wait for condition=available for deploy/%s in namespace %s: %s: %s", name, ns, err, o)
			}

			pods, err := TestHelper.GetPodsForDeployment(ns, name)
			if err != nil {
				testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to get pods for namespace %s", ns),
					"failed to get pods for namespace %s: %s", ns, err)
			}

			if len(pods) != 1 {
				testutil.Fatalf(t, "expected 1 pod for namespace %s, got %d", ns, len(pods))
			}

			shouldBeInjected := false
			switch nsAnnotation {
			case "", k8s.ProxyInjectDisabled:
				switch podAnnotation {
				case k8s.ProxyInjectEnabled:
					shouldBeInjected = true
				}
			case k8s.ProxyInjectEnabled:
				switch podAnnotation {
				case "", k8s.ProxyInjectEnabled:
					shouldBeInjected = true
				}
			}

			containers := pods[0].Spec.Containers
			initContainers := pods[0].Spec.InitContainers

			if shouldBeInjected {
				if len(containers) != 2 {
					testutil.Fatalf(t, "expected 2 containers for pod %s/%s, got %d", ns, pods[0].GetName(), len(containers))
				}
				if containers[0].Name != containerName && containers[1].Name != containerName {
					testutil.Fatalf(t, "expected bb-terminus container in pod %s/%s, got %+v", ns, pods[0].GetName(), containers[0])
				}
				if containers[0].Name != k8s.ProxyContainerName && containers[1].Name != k8s.ProxyContainerName {
					testutil.Fatalf(t, "expected %s container in pod %s/%s, got %+v", ns, pods[0].GetName(), k8s.ProxyContainerName, containers[0])
				}
				if !TestHelper.CNI() && len(initContainers) != 1 {
					testutil.Fatalf(t, "expected 1 init container for pod %s/%s, got %d", ns, pods[0].GetName(), len(initContainers))
				}
				if !TestHelper.CNI() && initContainers[0].Name != k8s.InitContainerName {
					testutil.Fatalf(t, "expected %s init container in pod %s/%s, got %+v", ns, pods[0].GetName(), k8s.InitContainerName, initContainers[0])
				}
			} else {
				if len(containers) != 1 {
					testutil.Fatalf(t, "expected 1 container for pod %s/%s, got %d", ns, pods[0].GetName(), len(containers))
				}
				if containers[0].Name != containerName {
					testutil.Fatalf(t, "expected bb-terminus container in pod %s/%s, got %s", ns, pods[0].GetName(), containers[0].Name)
				}
				if len(initContainers) != 0 {
					testutil.Fatalf(t, "expected 0 init containers for pod %s/%s, got %d", ns, pods[0].GetName(), len(initContainers))
				}
			}
		}
	}
}

func TestInjectAutoPod(t *testing.T) {
	podYAML, err := testutil.ReadFile("testdata/pod.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to read inject test file",
			"failed to read inject test file: %s", err)
	}

	injectNS := "inject-pod-test"
	podName := "inject-pod-test-terminus"
	nsAnnotations := map[string]string{
		k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
	}

	truthy := true
	falsy := false
	zero := int64(0)
	expectedInitContainer := v1.Container{
		Name:  k8s.InitContainerName,
		Image: "gcr.io/linkerd-io/proxy-init:" + version.ProxyInitVersion,
		Args: []string{
			"--incoming-proxy-port", "4143",
			"--outgoing-proxy-port", "4140",
			"--proxy-uid", "2102",
			// 1234,5678 were added at install time in `install_test.go`'s helmOverridesEdge()
			"--inbound-ports-to-ignore", "4190,4191,1234,5678",
		},
		Resources: v1.ResourceRequirements{
			Limits: v1.ResourceList{
				v1.ResourceName("cpu"):    resource.MustParse("100m"),
				v1.ResourceName("memory"): resource.MustParse("50Mi"),
			},
			Requests: v1.ResourceList{
				v1.ResourceName("cpu"):    resource.MustParse("10m"),
				v1.ResourceName("memory"): resource.MustParse("10Mi"),
			},
		},
		VolumeMounts: []v1.VolumeMount{
			{
				Name:      "linkerd-proxy-init-xtables-lock",
				ReadOnly:  false,
				MountPath: "/run",
			},
			{
				ReadOnly:  true,
				MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
			},
		},
		TerminationMessagePath: "/dev/termination-log",
		ImagePullPolicy:        "IfNotPresent",
		SecurityContext: &v1.SecurityContext{
			Capabilities: &v1.Capabilities{
				Add: []v1.Capability{v1.Capability("NET_ADMIN"), v1.Capability("NET_RAW")},
			},
			Privileged:               &falsy,
			RunAsUser:                &zero,
			RunAsNonRoot:             &falsy,
			AllowPrivilegeEscalation: &falsy,
			ReadOnlyRootFilesystem:   &truthy,
		},
		TerminationMessagePolicy: v1.TerminationMessagePolicy("FallbackToLogsOnError"),
	}

	ns := TestHelper.GetTestNamespace(injectNS)
	err = TestHelper.CreateDataPlaneNamespaceIfNotExists(ns, nsAnnotations)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to create namespace", "failed to create %s namespace: %s", ns, err)
	}

	o, err := TestHelper.Kubectl(podYAML, "--namespace", ns, "create", "-f", "-")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to create pod",
			"failed to create pod/%s in namespace %s for %s: %s", podName, ns, err, o)
	}

	o, err = TestHelper.Kubectl("", "--namespace", ns, "wait", "--for=condition=initialized", "--timeout=120s", "pod/"+podName)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to wait for condition=initialized",
			"failed to wait for condition=initialized for pod/%s in namespace %s: %s: %s", podName, ns, err, o)
	}

	pods, err := TestHelper.GetPods(ns, map[string]string{"app": podName})
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to get pods", "failed to get pods for namespace %s: %s", ns, err)
	}
	if len(pods) != 1 {
		testutil.Fatalf(t, "wrong number of pods returned for namespace %s: %d", ns, len(pods))
	}

	containers := pods[0].Spec.Containers
	if proxyContainer := testutil.GetProxyContainer(containers); proxyContainer == nil {
		testutil.Fatalf(t, "pod in namespace %s wasn't injected with the proxy container", ns)
	}

	if !TestHelper.CNI() {
		initContainers := pods[0].Spec.InitContainers
		if len(initContainers) == 0 {
			testutil.Fatalf(t, "pod in namespace %s wasn't injected with the init container", ns)
		}
		initContainer := initContainers[0]
		if mounts := initContainer.VolumeMounts; len(mounts) == 0 {
			testutil.AnnotatedFatalf(t, "init container doesn't have volume mounts", "init container doesn't have volume mounts: %#v", initContainer)
		}
		// Removed token volume name from comparison because it contains a random string
		initContainer.VolumeMounts[1].Name = ""
		if !reflect.DeepEqual(expectedInitContainer, initContainer) {
			testutil.AnnotatedFatalf(t, "malformed init container", "malformed init container:\nexpected:\n%#v\nactual:\n%#v", expectedInitContainer, initContainer)
		}
	}
}
