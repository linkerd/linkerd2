package healthcheck

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	// LinkerdGrafanaAddOnChecks adds checks related to grafana add-on components
	LinkerdGrafanaAddOnChecks CategoryID = "linkerd-grafana"

	// LinkerdPrometheusAddOnChecks adds checks related to Prometheus add-on components
	LinkerdPrometheusAddOnChecks CategoryID = "linkerd-prometheus"
)

var (
	// errorKeyNotFound is returned when a key is not found in data
	errorKeyNotFound error = errors.New("key not found")

	// AddOnCategories is the list of add-on category checks
	AddOnCategories = []CategoryID{LinkerdPrometheusAddOnChecks, LinkerdGrafanaAddOnChecks}
)

// addOnCategories contain all the checks w.r.t add-ons. It is strongly advised to
// have warning as true, to not make the check fail for add-on failures as most of them are
// not hard requirements unless otherwise.
func (hc *HealthChecker) addOnCategories() []category {
	return []category{
		{
			id: LinkerdPrometheusAddOnChecks,
			checkers: []checker{
				{
					description: "prometheus add-on service account exists",
					warning:     true,
					check: func(ctx context.Context) error {
						prometheusValues := make(map[string]interface{})
						err := yaml.Unmarshal(hc.linkerdConfig.Prometheus.Values(), &prometheusValues)
						if err != nil {
							return err
						}
						if GetBool(prometheusValues, "enabled") {
							return hc.checkServiceAccounts(ctx, []string{"linkerd-prometheus"}, hc.ControlPlaneNamespace, "")
						}
						return &SkipError{Reason: "prometheus add-on not enabled"}
					},
				},
				{
					description: "prometheus add-on config map exists",
					warning:     true,
					check: func(ctx context.Context) error {
						prometheusValues := make(map[string]interface{})
						err := yaml.Unmarshal(hc.linkerdConfig.Prometheus.Values(), &prometheusValues)
						if err != nil {
							return err
						}
						if GetBool(prometheusValues, "enabled") {
							_, err := hc.kubeAPI.CoreV1().ConfigMaps(hc.ControlPlaneNamespace).Get(ctx, "linkerd-prometheus-config", metav1.GetOptions{})
							return err
						}
						return &SkipError{Reason: "prometheus add-on not enabled"}
					},
				},
				{
					description:         "prometheus pod is running",
					warning:             true,
					retryDeadline:       hc.RetryDeadline,
					surfaceErrorOnRetry: true,
					check: func(ctx context.Context) error {
						prometheusValues := make(map[string]interface{})
						err := yaml.Unmarshal(hc.linkerdConfig.Prometheus.Values(), &prometheusValues)
						if err != nil {
							return err
						}
						if GetBool(prometheusValues, "enabled") {
							// populate controlPlanePods to get the latest status, during retries
							var err error
							hc.controlPlanePods, err = hc.kubeAPI.GetPodsByNamespace(ctx, hc.ControlPlaneNamespace)
							if err != nil {
								return err
							}

							return checkContainerRunning(hc.controlPlanePods, "prometheus")
						}
						return &SkipError{Reason: "prometheus add-on not enabled"}
					},
				},
			},
		},
		{
			id: LinkerdGrafanaAddOnChecks,
			checkers: []checker{
				{
					description: "grafana add-on service account exists",
					warning:     true,
					check: func(ctx context.Context) error {
						grafana := make(map[string]interface{})
						err := yaml.Unmarshal(hc.linkerdConfig.Grafana.Values(), &grafana)
						if err != nil {
							return err
						}
						if GetBool(grafana, "enabled") {
							name, err := GetString(grafana, "name")
							if err != nil && !errors.Is(err, errorKeyNotFound) {
								return err
							}

							if errors.Is(err, errorKeyNotFound) {
								// default name of grafana instance
								name = "linkerd-grafana"
							}

							return hc.checkServiceAccounts(ctx, []string{name}, hc.ControlPlaneNamespace, "")
						}
						return &SkipError{Reason: "grafana add-on not enabled"}
					},
				},
				{
					description: "grafana add-on config map exists",
					warning:     true,
					check: func(ctx context.Context) error {
						grafana := make(map[string]interface{})
						err := yaml.Unmarshal(hc.linkerdConfig.Grafana.Values(), &grafana)
						if err != nil {
							return err
						}
						if GetBool(grafana, "enabled") {
							name, err := GetString(grafana, "name")
							if err != nil && !errors.Is(err, errorKeyNotFound) {
								return err
							}

							if errors.Is(err, errorKeyNotFound) {
								// default name of grafana instance
								name = "linkerd-grafana"
							}

							_, err = hc.kubeAPI.CoreV1().ConfigMaps(hc.ControlPlaneNamespace).Get(ctx, fmt.Sprintf("%s-config", name), metav1.GetOptions{})
							if err != nil {
								return err
							}
							return nil
						}
						return &SkipError{Reason: "grafana add-on not enabled"}
					},
				},
				{
					description:         "grafana pod is running",
					warning:             true,
					retryDeadline:       hc.RetryDeadline,
					surfaceErrorOnRetry: true,
					check: func(ctx context.Context) error {
						grafana := make(map[string]interface{})
						err := yaml.Unmarshal(hc.linkerdConfig.Grafana.Values(), &grafana)
						if err != nil {
							return err
						}
						if GetBool(grafana, "enabled") {
							// populate controlPlanePods to get the latest status, during retries
							var err error
							hc.controlPlanePods, err = hc.kubeAPI.GetPodsByNamespace(ctx, hc.ControlPlaneNamespace)
							if err != nil {
								return err
							}

							return checkContainerRunning(hc.controlPlanePods, "grafana")
						}
						return &SkipError{Reason: "grafana add-on not enabled"}
					},
				},
			},
		},
	}
}

// GetString returns a String with the given key if present
func GetString(i interface{}, k string) (string, error) {
	m, ok := i.(map[string]interface{})
	if !ok {
		return "", errors.New("config value is not a map")
	}

	v, ok := m[k]
	if !ok {
		return "", errorKeyNotFound
	}

	res, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("config value '%v' for key '%s' is not a string", v, k)
	}

	return res, nil
}

// GetBool returns a bool with the given key if present.  Defaults to false if
// the key is not present or is a different type.
func GetBool(i interface{}, k string) bool {
	m, ok := i.(map[string]interface{})
	if !ok {
		return false
	}

	v, ok := m[k]
	if !ok {
		return false
	}

	res, ok := v.(bool)
	if !ok {
		return false
	}

	return res
}

// GetMap returns a Map with the given Key if Present
func GetMap(i interface{}, k string) (map[string]interface{}, error) {
	m, ok := i.(map[string]interface{})
	if !ok {
		return nil, errors.New("config value is not a map")
	}

	v, ok := m[k]
	if !ok {
		return nil, errorKeyNotFound
	}

	res, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("config value '%v' for key '%s' is not a map", v, k)
	}

	return res, nil
}
