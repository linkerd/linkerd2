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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// K8sValues gathers relevant heartbeat information from Kubernetes
func K8sValues(kubeAPI *k8s.KubernetesAPI, controlPlaneNamespace string) url.Values {
	v := url.Values{}

	config, err := healthcheck.FetchLinkerdConfigMap(kubeAPI, controlPlaneNamespace)
	if err != nil {
		log.Errorf("Failed to fetch linkerd-config: %s", err)
	} else {
		v.Set("uuid", config.GetInstall().GetUuid())
	}

	versionInfo, err := kubeAPI.GetVersionInfo()
	if err != nil {
		log.Errorf("Failed to fetch Kubernetes version info: %s", err)
	} else {
		v.Set("k8s-version", versionInfo.String())
	}

	ns, err := kubeAPI.CoreV1().Namespaces().Get(controlPlaneNamespace, metav1.GetOptions{})
	if err != nil {
		log.Errorf("Failed to fetch Linkerd namespace: %s", err)
	} else {
		v.Set("install-time", strconv.FormatInt(ns.GetCreationTimestamp().Unix(), 10))
	}

	return v
}

// PromValues gathers relevant heartbeat information from Prometheus
func PromValues(promAPI promv1.API) url.Values {
	v := url.Values{}

	value, err := promQuery(promAPI, "sum(irate(request_total{direction=\"inbound\"}[30s]))")
	if err != nil {
		log.Errorf("Prometheus query failed: %s", err)
	} else {
		v.Set("rps", value)
	}

	value, err = promQuery(promAPI, "count(count by (pod) (request_total))")
	if err != nil {
		log.Errorf("Prometheus query failed: %s", err)
	} else {
		v.Set("meshed-pods", value)
	}

	return v
}

func promQuery(promAPI promv1.API, query string) (string, error) {
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

		value := int64(math.Round(f))
		return strconv.FormatInt(value, 10), nil
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
