package smimetrics

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
)

type (
	TrafficMetricsList struct {
		Resource Resource         `json:"resource"`
		Items    []TrafficMetrics `json:"items"`
	}

	Resource struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}

	TrafficMetrics struct {
		Window   string   `json:"window"`
		Resource Resource `json:"resource"`
		Edge     Edge     `json:"edge"`
		Metrics  []Metric `json:"metrics"`
	}

	Edge struct {
		Direction string   `json:"direction"`
		Resource  Resource `json:"resource"`
	}

	Metric struct {
		Name  string `json:"name"`
		Unit  string `json:"unit"`
		Value string `json:"value"`
	}
)

func GetTrafficMetrics(k8sAPI *k8s.KubernetesAPI, namespace, kind, name string, params map[string]string) (*TrafficMetrics, error) {
	path := "/apis/metrics.smi-spec.io/v1alpha1"
	if namespace != "" {
		path = path + "/namespaces/" + namespace
	}
	path = path + "/" + kind
	path = path + "/" + name

	bytes, err := getMetricsResponse(k8sAPI, path, params)
	if err != nil {
		return nil, err
	}

	return parseTrafficMetrics(bytes)
}

func GetTrafficMetricsList(k8sAPI *k8s.KubernetesAPI, namespace, kind string, params map[string]string) (*TrafficMetricsList, error) {
	path := "/apis/metrics.smi-spec.io/v1alpha1"
	if namespace != "" {
		path = path + "/namespaces/" + namespace
	}
	path = path + "/" + kind

	bytes, err := getMetricsResponse(k8sAPI, path, params)
	if err != nil {
		return nil, err
	}

	return parseTrafficMetricsList(bytes)
}

func GetTrafficMetricsEdgesList(k8sAPI *k8s.KubernetesAPI, namespace, kind, name string, params map[string]string) (*TrafficMetricsList, error) {
	path := "/apis/metrics.smi-spec.io/v1alpha1"
	if namespace != "" {
		path = path + "/namespaces/" + namespace
	}
	path = path + "/" + kind
	path = path + "/" + name
	path = path + "/edges"

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

func parseTrafficMetrics(bytes []byte) (*TrafficMetrics, error) {
	var metrics TrafficMetrics
	err := json.Unmarshal(bytes, &metrics)
	if err != nil {
		log.Errorf("Failed to decode response as TrafficMetrics [%s]: %v", string(bytes), err)
		return nil, err
	}

	return &metrics, nil
}

func parseTrafficMetricsList(bytes []byte) (*TrafficMetricsList, error) {
	var metrics TrafficMetricsList
	err := json.Unmarshal(bytes, &metrics)
	if err != nil {
		log.Errorf("Failed to decode response as TrafficMetricsList [%s]: %v", string(bytes), err)
		return nil, err
	}

	return &metrics, nil
}
