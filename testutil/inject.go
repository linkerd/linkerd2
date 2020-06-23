package testutil

import (
	"fmt"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

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

func useTestImageTag(in string, h *TestHelper) (string, error) {
	patchOps := []string{
		fmt.Sprintf(`{"op": "replace", "path": "/spec/template/metadata/annotations/linkerd.io~1created-by", "value": "linkerd/cli %s"}`, h.GetVersion()),
		fmt.Sprintf(`{"op": "replace", "path": "/spec/template/metadata/annotations/linkerd.io~1proxy-version", "value": "%s"}`, h.GetVersion()),
		fmt.Sprintf(`{"op": "replace", "path": "/spec/template/spec/initContainers/0/image", "value": "init-image:%s"}`, version.ProxyInitVersion),
	}

	patchJSON := fmt.Sprintf("[%s]", strings.Join(patchOps, ","))
	return applyPatch(in, []byte(patchJSON))
}

// PatchDeploy patches a manifest by applying annotations
func PatchDeploy(in string, name string, annotations map[string]string) (string, error) {
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

// ValidateInject is similar to `TestHelper.ValidateOutput`, but it pins the
// image tag used in some annotations and that of the proxy-init container,
// which vary from build to build.
func ValidateInject(actual, fixtureFile string, h *TestHelper) error {
	actualPatched, err := useTestImageTag(actual, h)
	if err != nil {
		return err
	}

	fixture, err := ReadFile("testdata/" + fixtureFile)
	if err != nil {
		return err
	}
	fixturePatched, err := useTestImageTag(fixture, h)
	if err != nil {
		return err
	}

	if actualPatched != fixturePatched {
		return fmt.Errorf(
			"Expected:\n%s\nActual:\n%s", fixturePatched, actualPatched)
	}

	return nil
}

// GetProxyContainer get the proxy containers
func GetProxyContainer(containers []v1.Container) *v1.Container {
	for _, c := range containers {
		container := c
		if container.Name == k8s.ProxyContainerName {
			return &container
		}
	}

	return nil
}
