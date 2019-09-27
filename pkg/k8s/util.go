package k8s

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ExtensionAPIServerConfigMapResource returns the string representation of a ConfigMap resource, meant to be used in unit tests.
func ExtensionAPIServerConfigMapResource(data map[string]string) string {
	r := fmt.Sprintf(
		`
apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
  namespace: %s
data:
`, ExtensionAPIServerAuthenticationConfigMapName, metav1.NamespaceSystem)
	for k, v := range data {
		r = fmt.Sprintf("%s\n  %s: %s", r, k, v)
	}
	return r
}
