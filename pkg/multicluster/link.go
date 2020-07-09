package multicluster

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	consts "github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type (
	// ProbeSpec defines how a gateway should be queried for health. Once per
	// period, the probe workers will send an HTTP request to the remote gateway
	// on the given  port with the given path and expect a HTTP 200 response.
	ProbeSpec struct {
		Path   string
		Port   uint32
		Period time.Duration
	}

	// Link is an internal representation of the link.multicluster.linkerd.io
	// custom resource.  It defines a multicluster link to a gateway in a
	// target cluster and is configures the behavior of a service mirror
	// controller.
	Link struct {
		TargetClusterName             string
		TargetClusterDomain           string
		TargetClusterLinkerdNamespace string
		ClusterCredentialsSecret      string
		GatewayAddress                string
		GatewayPort                   uint32
		GatewayIdentity               string
		ProbeSpec                     ProbeSpec
	}
)

func (ps ProbeSpec) String() string {
	return fmt.Sprintf("ProbeSpec: {path: %s, port: %d, period: %s}", ps.Path, ps.Port, ps.Period)
}

// NewLink parses an unstructured link.multicluster.linkerd.io resource and
// converts it to a structured internal representation.
func NewLink(u unstructured.Unstructured) (Link, error) {

	spec, ok := u.Object["spec"]
	if !ok {
		return Link{}, errors.New("Field 'spec' is missing")
	}
	specObj, ok := spec.(map[string]interface{})
	if !ok {
		return Link{}, errors.New("Field 'spec' is not an object")
	}

	ps, ok := specObj["probeSpec"]
	if !ok {
		return Link{}, errors.New("Field 'probeSpec' is missing")
	}
	psObj, ok := ps.(map[string]interface{})
	if !ok {
		return Link{}, errors.New("Field 'probeSpec' it not an object")
	}

	probeSpec, err := newProbeSpec(psObj)
	if err != nil {
		return Link{}, err
	}

	targetClusterName, err := stringField(specObj, "targetClusterName")
	if err != nil {
		return Link{}, err
	}

	targetClusterDomain, err := stringField(specObj, "targetClusterDomain")
	if err != nil {
		return Link{}, err
	}

	targetClusterLinkerdNamespace, err := stringField(specObj, "targetClusterLinkerdNamespace")
	if err != nil {
		return Link{}, err
	}

	clusterCredentialsSecret, err := stringField(specObj, "clusterCredentialsSecret")
	if err != nil {
		return Link{}, err
	}

	gatewayAddress, err := stringField(specObj, "gatewayAddress")
	if err != nil {
		return Link{}, err
	}

	portStr, err := stringField(specObj, "gatewayPort")
	if err != nil {
		return Link{}, err
	}
	gatewayPort, err := strconv.ParseUint(portStr, 10, 32)
	if err != nil {
		return Link{}, err
	}

	gatewayIdentity, err := stringField(specObj, "gatewayIdentity")
	if err != nil {
		return Link{}, err
	}

	return Link{
		TargetClusterName:             targetClusterName,
		TargetClusterDomain:           targetClusterDomain,
		TargetClusterLinkerdNamespace: targetClusterLinkerdNamespace,
		ClusterCredentialsSecret:      clusterCredentialsSecret,
		GatewayAddress:                gatewayAddress,
		GatewayPort:                   uint32(gatewayPort),
		GatewayIdentity:               gatewayIdentity,
		ProbeSpec:                     probeSpec,
	}, nil
}

// ToUnstructured converts a Link struct into an unstructured resource that can
// be used by a kubernetes dynamic client.
func (l Link) ToUnstructured(name, namespace string) unstructured.Unstructured {
	return unstructured.Unstructured{

		Object: map[string]interface{}{
			"apiVersion": "multicluster.linkerd.io/v1alpha1",
			"kind":       "Link",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"targetClusterName":             l.TargetClusterName,
				"targetClusterDomain":           l.TargetClusterDomain,
				"targetClusterLinkerdNamespace": l.TargetClusterLinkerdNamespace,
				"clusterCredentialsSecret":      l.ClusterCredentialsSecret,
				"gatewayAddress":                l.GatewayAddress,
				"gatewayPort":                   fmt.Sprintf("%d", l.GatewayPort),
				"gatewayIdentity":               l.GatewayIdentity,
				"probeSpec": map[string]interface{}{
					"path":   l.ProbeSpec.Path,
					"port":   fmt.Sprintf("%d", l.ProbeSpec.Port),
					"period": l.ProbeSpec.Period.String(),
				},
			},
		},
	}
}

// ExtractProbeSpec parses the ProbSpec from a gateway service's annotations.
func ExtractProbeSpec(gateway *corev1.Service) (ProbeSpec, error) {
	path := gateway.Annotations[consts.GatewayProbePath]
	if path == "" {
		return ProbeSpec{}, errors.New("probe path is empty")
	}

	port, err := extractPort(gateway.Spec.Ports, consts.ProbePortName)
	if err != nil {
		return ProbeSpec{}, err
	}

	period, err := strconv.ParseUint(gateway.Annotations[consts.GatewayProbePeriod], 10, 32)
	if err != nil {
		return ProbeSpec{}, err
	}

	return ProbeSpec{
		Path:   path,
		Port:   port,
		Period: time.Duration(period) * time.Second,
	}, nil
}

func extractPort(port []corev1.ServicePort, portName string) (uint32, error) {
	for _, p := range port {
		if p.Name == portName {
			return uint32(p.Port), nil
		}
	}
	return 0, fmt.Errorf("could not find port with name %s", portName)
}

func newProbeSpec(obj map[string]interface{}) (ProbeSpec, error) {
	periodStr, err := stringField(obj, "period")
	if err != nil {
		return ProbeSpec{}, err
	}
	period, err := time.ParseDuration(periodStr)
	if err != nil {
		return ProbeSpec{}, err
	}

	path, err := stringField(obj, "path")
	if err != nil {
		return ProbeSpec{}, err
	}

	portStr, err := stringField(obj, "port")
	if err != nil {
		return ProbeSpec{}, err
	}
	port, err := strconv.ParseUint(portStr, 10, 32)
	if err != nil {
		return ProbeSpec{}, err
	}

	return ProbeSpec{
		Path:   path,
		Port:   uint32(port),
		Period: period,
	}, nil
}

func stringField(obj map[string]interface{}, key string) (string, error) {
	value, ok := obj[key]
	if !ok {
		return "", fmt.Errorf("Field '%s' is missing", key)
	}
	str, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("Field '%s' is not a string", key)
	}
	return str, nil
}
