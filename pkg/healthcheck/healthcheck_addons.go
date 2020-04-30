package healthcheck

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/labels"

	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	// LinkerdAddOnChecks adds checks to validate the add-on components
	LinkerdAddOnChecks CategoryID = "linkerd-addons"

	// LinkerdGrafanaAddOnChecks adds checks to validate the add-on components
	LinkerdGrafanaAddOnChecks CategoryID = "linkerd-grafana"
)

var (
	// AddOnCategories is the list of add-on category checks
	AddOnCategories = []CategoryID{LinkerdAddOnChecks, LinkerdGrafanaAddOnChecks}
)

func (hc *HealthChecker) addOnCategories() []category {
	return []category{
		{
			id: LinkerdAddOnChecks,
			checkers: []checker{
				{
					description: "linkerd add-on configmap exists",
					warning:     true,
					check: func(context.Context) error {
						return hc.checkIfAddOnsConfigMapExists()
					},
				},
			},
		},
		{
			id: LinkerdGrafanaAddOnChecks,
			checkers: []checker{
				{
					description: "grafana add-on service account exists",
					warning:     false,
					check: func(context.Context) error {
						if grafana, ok := hc.addOns[l5dcharts.GrafanaAddOn]; ok {
							// check for the grafana service account
							return hc.checkServiceAccounts([]string{grafana.(map[string]interface{})["name"].(string)})
						}
						return &SkipError{Reason: "grafana add-on not enabled"}
					},
				},
				{
					description: "grafana add-on configmap exists",
					warning:     false,
					check: func(context.Context) error {
						if grafana, ok := hc.addOns[l5dcharts.GrafanaAddOn]; ok {
							// check for the grafana config-map
							_, err := hc.kubeAPI.CoreV1().ConfigMaps(hc.ControlPlaneNamespace).Get(fmt.Sprintf("%s-config", grafana.(map[string]interface{})["name"].(string)), metav1.GetOptions{})
							if err != nil {
								return err
							}
							return nil
						}
						return &SkipError{Reason: "grafana add-on not enabled"}
					},
				},
				{
					description: "grafana pod is running",
					warning:     false,
					check: func(context.Context) error {
						if grafana, ok := hc.addOns[l5dcharts.GrafanaAddOn]; ok {

							// Get grafana add-on deployment
							deployment, err := hc.kubeAPI.AppsV1().Deployments(hc.ControlPlaneNamespace).Get(grafana.(map[string]interface{})["name"].(string), metav1.GetOptions{})
							if err != nil {
								return err
							}
							// Get grafana pod with match labels
							grafanaPods, err := hc.kubeAPI.CoreV1().Pods(hc.ControlPlaneNamespace).List(metav1.ListOptions{LabelSelector: labels.SelectorFromSet(deployment.Spec.Template.Labels).String()})
							if err != nil {
								return err
							}

							return checkContainerRunning(grafanaPods.Items, "grafana")
						}
						return &SkipError{Reason: "grafana add-on not enabled"}
					},
				},
			},
		},
	}
}

func (hc *HealthChecker) checkIfAddOnsConfigMapExists() error {

	// Check if linkerd-config-addons ConfigMap present, If no skip the next checks
	// If present update the add-on values for the next category to directly use
	cm, err := hc.checkForAddOnCM()
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

func (hc *HealthChecker) checkForAddOnCM() (string, error) {
	cm, err := k8s.GetAddOnsConfigMap(hc.kubeAPI, hc.ControlPlaneNamespace)
	if err != nil {
		return "", err
	}

	return cm["values"], nil
}
