package smimetrics

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
)

type (
	// TrafficMetricsList is a list of TrafficMetrics objects for a Resource.
	TrafficMetricsList struct {
		Resource Resource         `json:"resource"`
		Items    []TrafficMetrics `json:"items"`
	}

	// Resource names a Kubernetes resource.  If Name is blank, this refers to
	// a resource kind.  Otherwise, it refers to a specific named resource.
	Resource struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}

	// TrafficMetrics is a collection of metrics for a Resource.
	TrafficMetrics struct {
		Window   string   `json:"window"`
		Resource Resource `json:"resource"`
		Edge     Edge     `json:"edge"`
		Metrics  []Metric `json:"metrics"`
	}

	// Edge indicates if the associated metrics are measured on the inbound
	// or outbound side and which resource is the peer.
	Edge struct {
		Direction string   `json:"direction"`
		Resource  Resource `json:"resource"`
	}

	// Metric is an individual traffic metric.
	Metric struct {
		Name  string `json:"name"`
		Unit  string `json:"unit"`
		Value string `json:"value"`
	}

	// Status is an API response that is returned when the operation is not
	// successful.
	Status struct {
		Kind    string `json:"kind"`
		Status  string `json:"status"`
		Message string `json:"message"`
	}
)

const apiBase = "/apis/metrics.smi-spec.io/v1alpha1"

// GetTrafficMetrics returns the inbound traffic metrics for a specific named
// resource.
func GetTrafficMetrics(k8sAPI *k8s.KubernetesAPI, namespace, kind, name string, params map[string]string) (*TrafficMetrics, error) {
	ns := ""
	if namespace != "" {
		ns = "/namespaces/" + namespace
	}
	path := fmt.Sprintf("%s%s/%s/%s", apiBase, ns, kind, name)

	bytes, err := getMetricsResponse(k8sAPI, path, params)
	if err != nil {
		return nil, err
	}

	return parseTrafficMetrics(bytes)
}

// GetTrafficMetricsList returns the inbound traffic metrics for all resources
// of a given kind.
func GetTrafficMetricsList(k8sAPI *k8s.KubernetesAPI, namespace, kind string, params map[string]string) (*TrafficMetricsList, error) {
	ns := ""
	if namespace != "" {
		ns = "/namespaces/" + namespace
	}
	path := fmt.Sprintf("%s%s/%s", apiBase, ns, kind)

	bytes, err := getMetricsResponse(k8sAPI, path, params)
	if err != nil {
		return nil, err
	}

	return parseTrafficMetricsList(bytes)
}

// GetTrafficMetricsEdgesList returns the edge traffic metrics for a specific
// named resource.
func GetTrafficMetricsEdgesList(k8sAPI *k8s.KubernetesAPI, namespace, kind, name string, params map[string]string) (*TrafficMetricsList, error) {
	ns := ""
	if namespace != "" {
		ns = "/namespaces/" + namespace
	}
	path := fmt.Sprintf("%s%s/%s/%s/edges", apiBase, ns, kind, name)

	bytes, err := getMetricsResponse(k8sAPI, path, params)
	if err != nil {
		return nil, err
	}

	return parseTrafficMetricsList(bytes)
}

func getMetricsResponse(k8sAPI *k8s.KubernetesAPI, path string, params map[string]string) ([]byte, error) {
	client, err := k8sAPI.NewClient()
	if err != nil {
		return nil, err
	}

	url, err := url.Parse(k8sAPI.Host)
	if err != nil {
		return nil, err
	}

	url.Path = path

	for k, v := range params {
		url.Query().Add(k, v)
	}

	log.Debugf("Requesting %s", url)

	httpReq, err := http.NewRequest(
		http.MethodGet,
		url.String(),
		nil,
	)
	if err != nil {
		return nil, err
	}

	httpRsp, err := client.Do(httpReq)
	if err != nil {
		log.Debugf("Error invoking [%s]: %v", url, err)
		return nil, err
	}
	defer httpRsp.Body.Close()

	log.Debugf("Response from [%s] had headers: %v", url, httpRsp.Header)

	return ioutil.ReadAll(httpRsp.Body)
}

func handleStatusResponse(bytes []byte) error {
	var status Status
	json.Unmarshal(bytes, &status)
	if status.Kind == "Status" {
		return errors.New(status.Message)
	}
	return nil
}

func parseTrafficMetrics(bytes []byte) (*TrafficMetrics, error) {
	err := handleStatusResponse(bytes)
	if err != nil {
		return nil, err
	}

	var metrics TrafficMetrics
	err = json.Unmarshal(bytes, &metrics)
	if err != nil {
		log.Errorf("Failed to decode response as TrafficMetrics [%s]: %v", string(bytes), err)
		return nil, errors.New(string(bytes))
	}

	log.Debugf("Parsed TrafficMetrics: %+v", metrics)

	return &metrics, nil
}

func parseTrafficMetricsList(bytes []byte) (*TrafficMetricsList, error) {
	err := handleStatusResponse(bytes)
	if err != nil {
		return nil, err
	}

	var metrics TrafficMetricsList
	err = json.Unmarshal(bytes, &metrics)
	if err != nil {
		log.Errorf("Failed to decode response as TrafficMetricsList [%s]: %v", string(bytes), err)
		return nil, errors.New(string(bytes))
	}

	log.Debugf("Parsed TrafficMetricsList: %+v", metrics)

	return &metrics, nil
}
