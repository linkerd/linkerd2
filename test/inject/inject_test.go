package inject

import (
	"fmt"
	"os"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/linkerd/linkerd2/testutil"
	"sigs.k8s.io/yaml"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
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
		t.Fatalf("Unexpected error: %v: %s", stderr, err)
	}

	err = validateInject(out, "injected_default.golden")
	if err != nil {
		t.Fatalf("Received unexpected output\n%s", err.Error())
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
		t.Fatalf("Unexpected error: %v: %s", stderr, err)
	}

	err = validateInject(out, "injected_params.golden")
	if err != nil {
		t.Fatalf("Received unexpected output\n%s", err.Error())
	}
}

func TestInjectAutoNamespaceOverrideAnnotations(t *testing.T) {
	// Check for Namespace level override of proxy Configurations
	injectYAML, err := testutil.ReadFile("testdata/inject_test.yaml")
	if err != nil {
		t.Fatalf("failed to read inject test file: %s", err)
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
		t.Fatalf("failed to create %s namespace: %s", ns, err)
	}

	// patch injectYAML with unique name and pod annotations
	// Pod Level proxy configuration override
	podProxyCPUReq := "600m"
	podAnnotations := map[string]string{
		k8s.ProxyCPURequestAnnotation: podProxyCPUReq,
	}

	patchedYAML, err := patchDeploy(injectYAML, deployName, podAnnotations)
	if err != nil {
		t.Fatalf("failed to patch inject test YAML in namespace %s for deploy/%s: %s", ns, deployName, err)
	}

	o, err := TestHelper.Kubectl(patchedYAML, "--namespace", ns, "create", "-f", "-")
	if err != nil {
		t.Fatalf("failed to create deploy/%s in namespace %s for  %s: %s", deployName, ns, err, o)
	}

	o, err = TestHelper.Kubectl("", "--namespace", ns, "wait", "--for=condition=available", "--timeout=30s", "deploy/"+deployName)
	if err != nil {
		t.Fatalf("failed to wait for condition=available for deploy/%s in namespace %s: %s: %s", deployName, ns, err, o)
	}

	pods, err := TestHelper.GetPodsForDeployment(ns, deployName)
	if err != nil {
		t.Fatalf("failed to get pods for namespace %s: %s", ns, err)
	}

	containers := pods[0].Spec.Containers
	proxyContainer := getProxyContainer(containers)

	// Match the pod configuration with the namespace level overrides
	if proxyContainer.Resources.Requests["memory"] != resource.MustParse(nsProxyMemReq) {
		t.Fatalf("proxy memory resource request falied to match with namespace level override")
	}

	// Match with proxy level override
	if proxyContainer.Resources.Requests["cpu"] != resource.MustParse(podProxyCPUReq) {
		t.Fatalf("proxy cpu resource request falied to match with pod level override")
	}
}

func TestInjectAutoAnnotationPermutations(t *testing.T) {
	injectYAML, err := testutil.ReadFile("testdata/inject_test.yaml")
	if err != nil {
		t.Fatalf("failed to read inject test file: %s", err)
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
			t.Fatalf("failed to create %s namespace with annotation %s: %s", ns, nsAnnotation, err)
		}

		for _, podAnnotation := range injectAnnotations {
			// patch injectYAML with unique name and pod annotations
			name := deployName
			podAnnotations := map[string]string{}
			if podAnnotation != "" {
				podAnnotations[k8s.ProxyInjectAnnotation] = podAnnotation
				name = fmt.Sprintf("%s-%s", name, podAnnotation)
			}

			patchedYAML, err := patchDeploy(injectYAML, name, podAnnotations)
			if err != nil {
				t.Fatalf("failed to patch inject test YAML in namespace %s for deploy/%s: %s", ns, name, err)
			}

			o, err := TestHelper.Kubectl(patchedYAML, "--namespace", ns, "create", "-f", "-")
			if err != nil {
				t.Fatalf("failed to create deploy/%s in namespace %s for  %s: %s", name, ns, err, o)
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

			o, err := TestHelper.Kubectl("", "--namespace", ns, "wait", "--for=condition=available", "--timeout=30s", "deploy/"+name)
			if err != nil {
				t.Fatalf("failed to wait for condition=available for deploy/%s in namespace %s: %s: %s", name, ns, err, o)
			}

			pods, err := TestHelper.GetPodsForDeployment(ns, name)
			if err != nil {
				t.Fatalf("failed to get pods for namespace %s: %s", ns, err)
			}

			if len(pods) != 1 {
				t.Fatalf("expected 1 pod for namespace %s, got %d", ns, len(pods))
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
					t.Fatalf("expected 2 containers for pod %s/%s, got %d", ns, pods[0].GetName(), len(containers))
				}
				if containers[0].Name != containerName && containers[1].Name != containerName {
					t.Fatalf("expected bb-terminus container in pod %s/%s, got %+v", ns, pods[0].GetName(), containers[0])
				}
				if containers[0].Name != k8s.ProxyContainerName && containers[1].Name != k8s.ProxyContainerName {
					t.Fatalf("expected %s container in pod %s/%s, got %+v", ns, pods[0].GetName(), k8s.ProxyContainerName, containers[0])
				}
				if len(initContainers) != 1 {
					t.Fatalf("expected 1 init container for pod %s/%s, got %d", ns, pods[0].GetName(), len(initContainers))
				}
				if initContainers[0].Name != k8s.InitContainerName {
					t.Fatalf("expected %s init container in pod %s/%s, got %+v", ns, pods[0].GetName(), k8s.InitContainerName, initContainers[0])
				}
			} else {
				if len(containers) != 1 {
					t.Fatalf("expected 1 container for pod %s/%s, got %d", ns, pods[0].GetName(), len(containers))
				}
				if containers[0].Name != containerName {
					t.Fatalf("expected bb-terminus container in pod %s/%s, got %s", ns, pods[0].GetName(), containers[0].Name)
				}
				if len(initContainers) != 0 {
					t.Fatalf("expected 0 init containers for pod %s/%s, got %d", ns, pods[0].GetName(), len(initContainers))
				}
			}
		}
	}
}

func TestInjectAutoPod(t *testing.T) {
	podYAML, err := testutil.ReadFile("testdata/pod.yaml")
	if err != nil {
		t.Fatalf("failed to read inject test file: %s", err)
	}

	injectNS := "inject-pod-test"
	podName := "inject-pod-test-terminus"
	nsAnnotations := map[string]string{
		k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
	}

	ns := TestHelper.GetTestNamespace(injectNS)
	err = TestHelper.CreateDataPlaneNamespaceIfNotExists(ns, nsAnnotations)
	if err != nil {
		t.Fatalf("failed to create %s namespace: %s", ns, err)
	}

	o, err := TestHelper.Kubectl(podYAML, "--namespace", ns, "create", "-f", "-")
	if err != nil {
		t.Fatalf("failed to create pod/%s in namespace %s for %s: %s", podName, ns, err, o)
	}

	o, err = TestHelper.Kubectl("", "--namespace", ns, "wait", "--for=condition=initialized", "--timeout=30s", "pod/"+podName)
	if err != nil {
		t.Fatalf("failed to wait for condition=initialized for pod/%s in namespace %s: %s: %s", podName, ns, err, o)
	}

	pods, err := TestHelper.GetPods(ns, map[string]string{"app": podName})
	if err != nil {
		t.Fatalf("failed to get pods for namespace %s: %s", ns, err)
	}
	if len(pods) != 1 {
		t.Fatalf("wrong number of pods returned for namespace %s: %d", ns, len(pods))
	}

	containers := pods[0].Spec.Containers
	if proxyContainer := getProxyContainer(containers); proxyContainer == nil {
		t.Fatalf("pod in namespaces %s wasn't injected", ns)
	}
}

func applyPatch(in string, patchJSON []byte) (string, error) {
	patch, err := jsonpatch.DecodePatch(patchJSON)
	if err != nil {
		return "", err
	}
	json, err := yaml.YAMLToJSON([]byte(in))
	if err != nil {
		return "", err
	}
	patched, err := patch.Apply(json)
	if err != nil {
		return "", err
	}
	return string(patched), nil
}

func patchDeploy(in string, name string, annotations map[string]string) (string, error) {
	ops := []string{
		fmt.Sprintf(`{"op": "replace", "path": "/metadata/name", "value": "%s"}`, name),
		fmt.Sprintf(`{"op": "replace", "path": "/spec/selector/matchLabels/app", "value": "%s"}`, name),
		fmt.Sprintf(`{"op": "replace", "path": "/spec/template/metadata/labels/app", "value": "%s"}`, name),
	}

	if len(annotations) > 0 {
		ops = append(ops, `{"op": "add", "path": "/spec/template/metadata/annotations", "value": {}}`)
		for k, v := range annotations {
			ops = append(ops,
				fmt.Sprintf(`{"op": "add", "path": "/spec/template/metadata/annotations/%s", "value": "%s"}`, strings.Replace(k, "/", "~1", -1), v),
			)
		}
	}

	patchJSON := []byte(fmt.Sprintf("[%s]", strings.Join(ops, ",")))

	return applyPatch(in, patchJSON)
}

func useTestImageTag(in string) (string, error) {
	patchOps := []string{
		fmt.Sprintf(`{"op": "replace", "path": "/spec/template/metadata/annotations/linkerd.io~1created-by", "value": "linkerd/cli %s"}`, TestHelper.GetVersion()),
		fmt.Sprintf(`{"op": "replace", "path": "/spec/template/metadata/annotations/linkerd.io~1proxy-version", "value": "%s"}`, TestHelper.GetVersion()),
		fmt.Sprintf(`{"op": "replace", "path": "/spec/template/spec/initContainers/0/image", "value": "init-image:%s"}`, version.ProxyInitVersion),
	}

	patchJSON := fmt.Sprintf("[%s]", strings.Join(patchOps, ","))
	return applyPatch(in, []byte(patchJSON))
}

// validateInject is similar to `TestHelper.ValidateOutput`, but it pins the
// image tag used in some annotations and that of the proxy-init container,
// which vary from build to build.
func validateInject(actual, fixtureFile string) error {
	actualPatched, err := useTestImageTag(actual)
	if err != nil {
		return err
	}

	fixture, err := testutil.ReadFile("testdata/" + fixtureFile)
	if err != nil {
		return err
	}
	fixturePatched, err := useTestImageTag(fixture)
	if err != nil {
		return err
	}

	if actualPatched != fixturePatched {
		return fmt.Errorf(
			"Expected:\n%s\nActual:\n%s", fixturePatched, actualPatched)
	}

	return nil
}

// Get Proxy Container from Containers
func getProxyContainer(containers []v1.Container) *v1.Container {
	for _, c := range containers {
		container := c
		if container.Name == k8s.ProxyContainerName {
			return &container
		}
	}

	return nil
}
