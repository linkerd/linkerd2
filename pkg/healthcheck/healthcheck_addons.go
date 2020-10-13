package healthcheck

import (
	"context"
	"errors"
	"fmt"

	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// LinkerdGrafanaAddOnChecks adds checks related to grafana add-on components
	LinkerdGrafanaAddOnChecks CategoryID = "linkerd-grafana"

	// LinkerdPrometheusAddOnChecks adds checks related to Prometheus add-on components
	LinkerdPrometheusAddOnChecks CategoryID = "linkerd-prometheus"

	// LinkerdTracingAddOnChecks adds checks related to tracing add-on components
	LinkerdTracingAddOnChecks CategoryID = "linkerd-tracing"
)

var (
	// errorKeyNotFound is returned when a key is not found in data
	errorKeyNotFound error = errors.New("key not found")

	// AddOnCategories is the list of add-on category checks
	AddOnCategories = []CategoryID{LinkerdPrometheusAddOnChecks, LinkerdGrafanaAddOnChecks, LinkerdTracingAddOnChecks}
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
						if _, ok := hc.addOns[l5dcharts.PrometheusAddOn]; ok {
							return hc.checkServiceAccounts(ctx, []string{"linkerd-prometheus"}, hc.ControlPlaneNamespace, "")
						}
						return &SkipError{Reason: "prometheus add-on not enabled"}
					},
				},
				{
					description: "prometheus add-on config map exists",
					warning:     true,
					check: func(ctx context.Context) error {
						if _, ok := hc.addOns[l5dcharts.PrometheusAddOn]; ok {
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
						if _, ok := hc.addOns[l5dcharts.PrometheusAddOn]; ok {
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
						if grafana, ok := hc.addOns[l5dcharts.GrafanaAddOn]; ok {
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
						if grafana, ok := hc.addOns[l5dcharts.GrafanaAddOn]; ok {
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
						if _, ok := hc.addOns[l5dcharts.GrafanaAddOn]; ok {
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
		{
			id: LinkerdTracingAddOnChecks,
			checkers: []checker{
				{
					description: "collector service account exists",
					warning:     true,
					check: func(ctx context.Context) error {
						if tracing, ok := hc.addOns[l5dcharts.TracingAddOn]; ok {

							collector, mapError := GetMap(tracing, "collector")

							collectorName, keyError := GetString(collector, "name")

							if errors.Is(mapError, errorKeyNotFound) || errors.Is(keyError, errorKeyNotFound) {
								// default name of collector instance
								collectorName = "linkerd-collector"
							} else {
								if mapError != nil {
									return mapError
								}
								if keyError != nil {
									return keyError
								}
							}

							return hc.checkServiceAccounts(ctx, []string{collectorName}, hc.ControlPlaneNamespace, "")
						}
						return &SkipError{Reason: "tracing add-on not enabled"}
					},
				},
				{
					description: "jaeger service account exists",
					warning:     true,
					check: func(ctx context.Context) error {
						if tracing, ok := hc.addOns[l5dcharts.TracingAddOn]; ok {
							jaeger, mapError := GetMap(tracing, "jaeger")

							jaegerName, keyError := GetString(jaeger, "name")

							if errors.Is(mapError, errorKeyNotFound) || errors.Is(keyError, errorKeyNotFound) {
								// default name of jaeger instance
								jaegerName = "linkerd-jaeger"
							} else {
								if mapError != nil {
									return mapError
								}
								if keyError != nil {
									return keyError
								}
							}

							return hc.checkServiceAccounts(ctx, []string{jaegerName}, hc.ControlPlaneNamespace, "")
						}
						return &SkipError{Reason: "tracing add-on not enabled"}
					},
				},
				{
					description: "collector config map exists",
					warning:     true,
					check: func(ctx context.Context) error {
						if tracing, ok := hc.addOns[l5dcharts.TracingAddOn]; ok {
							collector, mapError := GetMap(tracing, "collector")

							collectorName, keyError := GetString(collector, "name")

							if errors.Is(mapError, errorKeyNotFound) || errors.Is(keyError, errorKeyNotFound) {
								// default name of collector instance
								collectorName = "linkerd-collector"
							} else {
								if mapError != nil {
									return mapError
								}
								if keyError != nil {
									return keyError
								}
							}

							_, err := hc.kubeAPI.CoreV1().ConfigMaps(hc.ControlPlaneNamespace).Get(ctx, fmt.Sprintf("%s-config", collectorName), metav1.GetOptions{})
							if err != nil {
								return err
							}
							return nil
						}
						return &SkipError{Reason: "tracing add-on not enabled"}
					},
				},
				{
					description:         "collector pod is running",
					warning:             true,
					retryDeadline:       hc.RetryDeadline,
					surfaceErrorOnRetry: true,
					check: func(ctx context.Context) error {
						if _, ok := hc.addOns[l5dcharts.TracingAddOn]; ok {
							// populate controlPlanePods to get the latest status, during retries
							var err error
							hc.controlPlanePods, err = hc.kubeAPI.GetPodsByNamespace(ctx, hc.ControlPlaneNamespace)
							if err != nil {
								return err
							}

							return checkContainerRunning(hc.controlPlanePods, "collector")
						}
						return &SkipError{Reason: "tracing add-on not enabled"}
					},
				},
				{
					description:         "jaeger pod is running",
					warning:             true,
					retryDeadline:       hc.RetryDeadline,
					surfaceErrorOnRetry: true,
					check: func(ctx context.Context) error {
						if _, ok := hc.addOns[l5dcharts.TracingAddOn]; ok {
							// populate controlPlanePods to get the latest status, during retries
							var err error
							hc.controlPlanePods, err = hc.kubeAPI.GetPodsByNamespace(ctx, hc.ControlPlaneNamespace)
							if err != nil {
								return err
							}

							return checkContainerRunning(hc.controlPlanePods, "jaeger")
						}
						return &SkipError{Reason: "tracing add-on not enabled"}
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
