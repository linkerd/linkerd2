package config

import (
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	log "github.com/sirupsen/logrus"
	"sigs.k8s.io/yaml"
)

// Values returns the Value struct from the linkerd-config ConfigMap
func Values(path, prefix string) (*l5dcharts.Values, error) {
	path = filepath.Clean(path)
	if !strings.HasPrefix(path, prefix) {
		return nil, fmt.Errorf("invalid path: %s", path)
	}
	// path has been validated
	//nolint:gosec
	configYaml, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	log.Debugf("%s config YAML: %s", path, configYaml)

	values := &l5dcharts.Values{}
	if err = yaml.Unmarshal(configYaml, values); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON from: %s: %w", path, err)
	}
	return values, err
}

// RemoveGlobalFieldIfPresent removes the `global` node and
// attaches the children nodes there.
func RemoveGlobalFieldIfPresent(bytes []byte) ([]byte, error) {
	// Check if Globals is present and remove that node if it has
	var valuesMap map[string]interface{}
	err := yaml.Unmarshal(bytes, &valuesMap)
	if err != nil {
		return nil, err
	}

	if globalValues, ok := valuesMap["global"]; ok {
		// attach those values
		// Check if its a map
		if val, ok := globalValues.(map[string]interface{}); ok {
			for k, v := range val {
				valuesMap[k] = v
			}
		}
		// Remove global now
		delete(valuesMap, "global")
	}

	bytes, err = yaml.Marshal(valuesMap)
	if err != nil {
		return nil, err
	}

	return bytes, nil
}

// FetchLinkerdConfigMap retrieves the `linkerd-config` ConfigMap from
// Kubernetes.
func FetchLinkerdConfigMap(ctx context.Context, k kubernetes.Interface, controlPlaneNamespace string) (*corev1.ConfigMap, error) {
	cm, err := k.CoreV1().ConfigMaps(controlPlaneNamespace).Get(ctx, k8s.ConfigConfigMapName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return cm, nil
}
