package healthcheck

import (
	"context"
	"errors"
	"fmt"

	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	// LinkerdAddOnChecks adds checks to validate the add-on components
	LinkerdAddOnChecks CategoryID = "linkerd-addons"

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
	AddOnCategories = []CategoryID{LinkerdAddOnChecks, LinkerdPrometheusAddOnChecks, LinkerdGrafanaAddOnChecks, LinkerdTracingAddOnChecks}
)

// addOnCategories contain all the checks w.r.t add-ons. It is strongly advised to
// have warning as true, to not make the check fail for add-on failures as most of them are
// not hard requirements unless otherwise.
func (hc *HealthChecker) addOnCategories() []category {
	return []category{
		{
			id: LinkerdAddOnChecks,
			checkers: []checker{
				{
					description: fmt.Sprintf("'%s' config map exists", k8s.AddOnsConfigMapName),
					warning:     true,
					check: func(ctx context.Context) error {
						return hc.checkIfAddOnsConfigMapExists(ctx)
					},
				},
			},
		},
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
							name, err := getString(grafana, "name")
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
							name, err := getString(grafana, "name")
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

							collector, mapError := getMap(tracing, "collector")

							collectorName, keyError := getString(collector, "name")

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
							jaeger, mapError := getMap(tracing, "jaeger")

							jaegerName, keyError := getString(jaeger, "name")

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
							collector, mapError := getMap(tracing, "collector")

							collectorName, keyError := getString(collector, "name")

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

func (hc *HealthChecker) checkIfAddOnsConfigMapExists(ctx context.Context) error {

	// Check if linkerd-config-addons ConfigMap present, If not skip the next checks
	cm, err := hc.checkForAddOnCM(ctx)
	if err != nil {
		return err
	}

	// linkerd-config-addons cm is present,now update hc to include those add-ons
	// so that further add-on specific checks can be ran
	var values l5dcharts.Values
	err = yaml.Unmarshal([]byte(cm), &values)
	if err != nil {
		return fmt.Errorf("could not unmarshal %s config-map: %s", k8s.AddOnsConfigMapName, err)
	}

	addOns, err := l5dcharts.ParseAddOnValues(&values)
	if err != nil {
		return fmt.Errorf("could not read %s config-map: %s", k8s.AddOnsConfigMapName, err)
	}

	hc.addOns = make(map[string]interface{})

	for _, addOn := range addOns {
		values := map[string]interface{}{}
		err = yaml.Unmarshal(addOn.Values(), &values)
		if err != nil {
			return err
		}

		hc.addOns[addOn.Name()] = values
	}

	return nil
}

func (hc *HealthChecker) checkForAddOnCM(ctx context.Context) (string, error) {
	cm, err := k8s.GetAddOnsConfigMap(ctx, hc.kubeAPI, hc.ControlPlaneNamespace)
	if err != nil {
		return "", err
	}

	values, ok := cm["values"]
	if !ok {
		return "", fmt.Errorf("values subpath not found in %s configmap", k8s.AddOnsConfigMapName)
	}

	return values, nil
}

func getString(i interface{}, k string) (string, error) {
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

func getMap(i interface{}, k string) (map[string]interface{}, error) {
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
