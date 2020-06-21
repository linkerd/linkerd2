package testutil

import (
	"fmt"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
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
