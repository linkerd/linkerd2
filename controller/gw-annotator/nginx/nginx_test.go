package nginx

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/controller/gw-annotator/gateway"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestNeedsAnnotation(t *testing.T) {
	testCases := []struct {
		desc           string
		annotations    map[string]interface{}
		expectedOutput bool
	}{
		{
			desc:           "no annotations",
			annotations:    nil,
			expectedOutput: true,
		},
		{
			desc: "empty nginx configuration snippet annotation",
			annotations: map[string]interface{}{
				DefaultPrefix + ConfigSnippetKey: "",
			},
			expectedOutput: true,
		},
		{
			desc: "nginx configuration snippet annotation present but no l5d header",
			annotations: map[string]interface{}{
				DefaultPrefix + ConfigSnippetKey: "entry1",
			},
			expectedOutput: true,
		},
		{
			desc: "invalid l5d header for http traffic",
			annotations: map[string]interface{}{
				DefaultPrefix + ConfigSnippetKey: "proxy_set_header l5d-dst-override",
			},
			expectedOutput: true,
		},
		{
			desc: "another invalid l5d header for http traffic",
			annotations: map[string]interface{}{
				DefaultPrefix + ConfigSnippetKey: "proxy_set_header l5d-dst-overide test",
			},
			expectedOutput: true,
		},
		{
			desc: "valid l5d header for http traffic",
			annotations: map[string]interface{}{
				DefaultPrefix + ConfigSnippetKey: L5DHeaderTestsValueHTTP,
			},
			expectedOutput: false,
		},
		{
			desc: "valid l5d header for http traffic (not using default annotation prefix)",
			annotations: map[string]interface{}{
				"custom-prefix" + ConfigSnippetKey: L5DHeaderTestsValueHTTP,
			},
			expectedOutput: false,
		},
		{
			desc: "valid l5d header for grpc traffic",
			annotations: map[string]interface{}{
				DefaultPrefix + ConfigSnippetKey: L5DHeaderTestsValueGRPC,
			},
			expectedOutput: false,
		},
		{
			desc: "valid l5d header for http and grpc traffic",
			annotations: map[string]interface{}{
				DefaultPrefix + ConfigSnippetKey: L5DHeaderTestsValueHTTP + "\n" + L5DHeaderTestsValueGRPC,
			},
			expectedOutput: false,
		},
	}

	for i, tc := range testCases {
		tc := tc
		t.Run(fmt.Sprintf("test_%d: %s", i, tc.desc), func(t *testing.T) {
			obj := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"annotations": tc.annotations,
					},
				},
			}
			g := &Gateway{Object: obj}
			output := g.NeedsAnnotation()
			if output != tc.expectedOutput {
				t.Errorf("expecting output to be %v but got %v", tc.expectedOutput, output)
			}
		})
	}
}

func TestGenerateAnnotationPatch(t *testing.T) {
	annotationKey := DefaultPrefix + ConfigSnippetKey
	annotationPath := gateway.AnnotationsPath + strings.Replace(annotationKey, "/", "~1", -1)

	testCases := []struct {
		desc           string
		annotations    map[string]interface{}
		clusterDomain  string
		expectedOutput gateway.Patch
	}{
		{
			desc:          "no annotations",
			annotations:   nil,
			clusterDomain: gateway.DefaultClusterDomain,
			expectedOutput: []gateway.PatchOperation{{
				Op:    "add",
				Path:  annotationPath,
				Value: fmt.Sprintf("%s\n%s", L5DHeaderTestsValueHTTP, L5DHeaderTestsValueGRPC),
			}},
		},
		{
			desc: "no nginx configuration snippet annotation",
			annotations: map[string]interface{}{
				"k1": "v1",
			},
			clusterDomain: gateway.DefaultClusterDomain,
			expectedOutput: []gateway.PatchOperation{{
				Op:    "add",
				Path:  annotationPath,
				Value: fmt.Sprintf("%s\n%s", L5DHeaderTestsValueHTTP, L5DHeaderTestsValueGRPC),
			}},
		},
		{
			desc: "no nginx configuration snippet annotation (using custom cluster domain)",
			annotations: map[string]interface{}{
				"k1": "v1",
			},
			clusterDomain: "my-domain.org",
			expectedOutput: []gateway.PatchOperation{{
				Op:   "add",
				Path: annotationPath,
				Value: fmt.Sprintf("%s\n%s",
					strings.Replace(L5DHeaderTestsValueHTTP, gateway.DefaultClusterDomain, "my-domain.org", -1),
					strings.Replace(L5DHeaderTestsValueGRPC, gateway.DefaultClusterDomain, "my-domain.org", -1),
				),
			}},
		},
		{
			desc: "empty nginx configuration snippet annotation",
			annotations: map[string]interface{}{
				DefaultPrefix + ConfigSnippetKey: "",
			},
			clusterDomain: gateway.DefaultClusterDomain,
			expectedOutput: []gateway.PatchOperation{{
				Op:    "replace",
				Path:  annotationPath,
				Value: fmt.Sprintf("%s\n%s", L5DHeaderTestsValueHTTP, L5DHeaderTestsValueGRPC),
			}},
		},
		{
			desc: "nginx configuration snippet annotation has some entries but not l5d ones",
			annotations: map[string]interface{}{
				DefaultPrefix + ConfigSnippetKey: "\nentry1;\nentry2;",
			},
			clusterDomain: gateway.DefaultClusterDomain,
			expectedOutput: []gateway.PatchOperation{{
				Op:    "replace",
				Path:  annotationPath,
				Value: fmt.Sprintf("entry1;\nentry2;\n%s\n%s", L5DHeaderTestsValueHTTP, L5DHeaderTestsValueGRPC),
			}},
		},
		{
			desc: "nginx configuration snippet annotation has some entries but not l5d ones (trailing new line)",
			annotations: map[string]interface{}{
				DefaultPrefix + ConfigSnippetKey: "entry1;\nentry2;\n",
			},
			clusterDomain: gateway.DefaultClusterDomain,
			expectedOutput: []gateway.PatchOperation{{
				Op:    "replace",
				Path:  annotationPath,
				Value: fmt.Sprintf("entry1;\nentry2;\n%s\n%s", L5DHeaderTestsValueHTTP, L5DHeaderTestsValueGRPC),
			}},
		},
	}

	for i, tc := range testCases {
		tc := tc
		t.Run(fmt.Sprintf("test_%d: %s", i, tc.desc), func(t *testing.T) {
			obj := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"annotations": tc.annotations,
					},
				},
			}
			g := &Gateway{Object: obj}
			output, _ := g.GenerateAnnotationPatch(tc.clusterDomain)
			if !reflect.DeepEqual(output, tc.expectedOutput) {
				t.Errorf("expecting output to be\n %v\n but got\n %v", tc.expectedOutput, output)
			}
		})
	}
}
