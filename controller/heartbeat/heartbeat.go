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

	pkgK8s "github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type containerMeta struct {
	name model.LabelValue
	ns   model.LabelValue
}

// K8sValues gathers relevant heartbeat information from Kubernetes
func K8sValues(ctx context.Context, kubeAPI *k8s.KubernetesAPI, controlPlaneNamespace string) url.Values {
	v := url.Values{}

	cm, _, err := healthcheck.FetchLinkerdConfigMap(ctx, kubeAPI, controlPlaneNamespace)
	if err != nil {
		log.Errorf("Failed to fetch linkerd-config: %s", err)
	} else {
		v.Set("uuid", string(cm.GetUID()))
		v.Set("install-time", strconv.FormatInt(cm.GetCreationTimestamp().Unix(), 10))
	}

	versionInfo, err := kubeAPI.GetVersionInfo()
	if err != nil {
		log.Errorf("Failed to fetch Kubernetes version info: %s", err)
	} else {
		v.Set("k8s-version", versionInfo.String())
	}

	namespaces, err := kubeAPI.GetAllNamespacesWithExtensionLabel(ctx)
	if err != nil {
		log.Errorf("Failed to fetch namespaces with %s label: %s", k8s.LinkerdExtensionLabel, err)
	} else {
		for _, ns := range namespaces {
			extensionNameParam := fmt.Sprintf("ext-%s", ns.Labels[k8s.LinkerdExtensionLabel])
			v.Set(extensionNameParam, "1")
		}
	}

	err = k8s.ServiceProfilesAccess(ctx, kubeAPI)
	if err != nil {
		log.Errorf("Failed to verify service profile access: %s", err)
		return v
	}

	spClient, err := pkgK8s.NewSpClientSet(kubeAPI.Config)
	if err != nil {
		log.Errorf("Failed to create service profile client: %s", err)
		return v
	}

	spList, err := spClient.LinkerdV1alpha2().ServiceProfiles("").List(ctx, v1.ListOptions{})
	if err != nil {
		log.Errorf("Failed to get service profiles: %s", err)
		return v
	}

	v.Set("service-profile-count", strconv.Itoa(len(spList.Items)))

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
	for _, container := range []containerMeta{
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
		// as of k8s 1.16 cadvisor labels container names with just `container`
		containerLabelsPre16 := getLabelSet(container, "container_name")
		containerLabelsPost16 := getLabelSet(container, "container")

		// max-mem
		query = fmt.Sprintf("max(container_memory_working_set_bytes%s or container_memory_working_set_bytes%s)",
			containerLabelsPre16, containerLabelsPost16)
		value, err = promQuery(promAPI, query, 0)
		if err != nil {
			log.Errorf("Prometheus query failed: %s", err)
		} else {
			param := fmt.Sprintf("max-mem-%s", container.name)
			v.Set(param, value)
		}

		// p95-cpu
		query = fmt.Sprintf("max(quantile_over_time(0.95,rate(container_cpu_usage_seconds_total%s[5m])[24h:5m]) "+
			"or quantile_over_time(0.95,rate(container_cpu_usage_seconds_total%s[5m])[24h:5m]))",
			containerLabelsPre16, containerLabelsPost16)
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

func getLabelSet(container containerMeta, containerKey model.LabelName) model.LabelSet {
	containerLabels := model.LabelSet{
		"job":        "kubernetes-nodes-cadvisor",
		containerKey: container.name,
	}
	if container.ns != "" {
		containerLabels["namespace"] = container.ns
	}
	return containerLabels
}

func promQuery(promAPI promv1.API, query string, precision int) (string, error) {
	log.Debugf("Prometheus query: %s", query)

	res, warn, err := promAPI.Query(context.Background(), query, time.Time{})
	if err != nil {
		return "", err
	}
	if warn != nil {
		log.Warnf("%v", warn)
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
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("request failed with code %d; response body: %s", resp.StatusCode, string(body))
	}

	log.Infof("Successfully sent heartbeat: %s", string(body))

	return nil
}
