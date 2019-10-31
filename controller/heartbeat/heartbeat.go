package heartbeat

import (
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
)

// K8sValues gathers relevant heartbeat information from Kubernetes
func K8sValues(kubeAPI *k8s.KubernetesAPI, controlPlaneNamespace string) url.Values {
	v := url.Values{}

	cm, configPB, err := healthcheck.FetchLinkerdConfigMap(kubeAPI, controlPlaneNamespace)
	if err != nil {
		log.Errorf("Failed to fetch linkerd-config: %s", err)
	} else {
		v.Set("uuid", configPB.GetInstall().GetUuid())
		v.Set("install-time", strconv.FormatInt(cm.GetCreationTimestamp().Unix(), 10))
	}

	versionInfo, err := kubeAPI.GetVersionInfo()
	if err != nil {
		log.Errorf("Failed to fetch Kubernetes version info: %s", err)
	} else {
		v.Set("k8s-version", versionInfo.String())
	}

	return v
}

// PromValues gathers relevant heartbeat information from Prometheus
func PromValues(promAPI promv1.API, controlPlaneNamespace string) url.Values {
	v := url.Values{}

	jobProxyLabels := model.LabelSet{"job": "linkerd-proxy"}

	// total-rps
	query := fmt.Sprintf("sum(rate(request_total%s[30s]))", jobProxyLabels.Merge(model.LabelSet{"direction": "inbound"}))
	value, err := promQuery(promAPI, query, 0)
	if err != nil {
		log.Errorf("Prometheus query failed: %s", err)
	} else {
		v.Set("total-rps", value)
	}

	// meshed-pods
	query = fmt.Sprintf("count(count by (pod) (request_total%s))", jobProxyLabels)
	value, err = promQuery(promAPI, query, 0)
	if err != nil {
		log.Errorf("Prometheus query failed: %s", err)
	} else {
		v.Set("meshed-pods", value)
	}

	// p95-handle-us
	query = fmt.Sprintf("histogram_quantile(0.99, sum(rate(request_handle_us_bucket%s[24h])) by (le))", jobProxyLabels)
	value, err = promQuery(promAPI, query, 0)
	if err != nil {
		log.Errorf("Prometheus query failed: %s", err)
	} else {
		v.Set("p99-handle-us", value)
	}

	// proxy-injector-injections
	jobInjectorLabels := model.LabelSet{
		"job":  "linkerd-controller",
		"skip": "false",
	}
	query = fmt.Sprintf("sum(proxy_inject_admission_responses_total%s)", jobInjectorLabels)
	value, err = promQuery(promAPI, query, 0)
	if err != nil {
		log.Errorf("Prometheus query failed: %s", err)
	} else {
		v.Set("proxy-injector-injections", value)
	}

	// container metrics
	for _, container := range []struct {
		name model.LabelValue
		ns   model.LabelValue
	}{
		{
			name: "linkerd-proxy",
		},
		{
			name: "destination",
			ns:   "linkerd",
		},
		{
			name: "prometheus",
			ns:   "linkerd",
		},
	} {
		containerLabels := model.LabelSet{
			"job":            "kubernetes-nodes-cadvisor",
			"container_name": container.name,
		}
		if container.ns != "" {
			containerLabels["namespace"] = container.ns
		}

		// max-mem
		query = fmt.Sprintf("max(container_memory_working_set_bytes%s)", containerLabels)
		value, err = promQuery(promAPI, query, 0)
		if err != nil {
			log.Errorf("Prometheus query failed: %s", err)
		} else {
			param := fmt.Sprintf("max-mem-%s", container.name)
			v.Set(param, value)
		}

		// p95-cpu
		query = fmt.Sprintf("max(quantile_over_time(0.95,rate(container_cpu_usage_seconds_total%s[5m])[24h:5m]))", containerLabels)
		value, err = promQuery(promAPI, query, 3)
		if err != nil {
			log.Errorf("Prometheus query failed: %s", err)
		} else {
			param := fmt.Sprintf("p95-cpu-%s", container.name)
			v.Set(param, value)
		}
	}

	return v
}

func promQuery(promAPI promv1.API, query string, precision int) (string, error) {
	log.Debugf("Prometheus query: %s", query)

	res, err := promAPI.Query(context.Background(), query, time.Time{})
	if err != nil {
		return "", err
	}

	switch result := res.(type) {
	case model.Vector:
		if len(result) != 1 {
			return "", fmt.Errorf("unexpected result Prometheus result vector length: %d", len(result))
		}
		f := float64(result[0].Value)
		if math.IsNaN(f) {
			return "", fmt.Errorf("unexpected sample value: %v", result[0].Value)
		}

		return strconv.FormatFloat(f, 'f', precision, 64), nil
	}

	return "", fmt.Errorf("unexpected query result type (expected Vector): %s", res.Type())
}

// MergeValues merges two url.Values
func MergeValues(v1, v2 url.Values) url.Values {
	v := url.Values{}
	for key, val := range v1 {
		v[key] = val
	}
	for key, val := range v2 {
		v[key] = val
	}
	return v
}

// Send takes a map of url.Values and sends them to versioncheck.linkerd.io
func Send(v url.Values) error {
	return send(http.DefaultClient, version.CheckURL, v)
}

func send(client *http.Client, baseURL string, v url.Values) error {
	req, err := http.NewRequest("GET", baseURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request for base URL [%s]: %s", baseURL, err)
	}
	req.URL.RawQuery = v.Encode()

	log.Infof("Sending heartbeat: %s", req.URL.String())
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Check URL [%s] request failed with: %s", req.URL.String(), err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %s", err)
	}

	log.Infof("Successfully sent heartbeat: %s", string(body))

	return nil
}
