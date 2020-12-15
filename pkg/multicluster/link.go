package multicluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
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
		Name                          string
		Namespace                     string
		TargetClusterName             string
		TargetClusterDomain           string
		TargetClusterLinkerdNamespace string
		ClusterCredentialsSecret      string
		GatewayAddress                string
		GatewayPort                   uint32
		GatewayIdentity               string
		ProbeSpec                     ProbeSpec
		Selector                      metav1.LabelSelector
	}
)

// LinkGVR is the Group Version and Resource of the Link custom resource.
var LinkGVR = schema.GroupVersionResource{
	Group:    k8s.LinkAPIGroup,
	Version:  k8s.LinkAPIVersion,
	Resource: "links",
}

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

	selector := metav1.LabelSelector{}
	if selectorObj, ok := specObj["selector"]; ok {
		bytes, err := json.Marshal(selectorObj)
		if err != nil {
			return Link{}, err
		}
		err = json.Unmarshal(bytes, &selector)
		if err != nil {
			return Link{}, err
		}
	}

	return Link{
		Name:                          u.GetName(),
		Namespace:                     u.GetNamespace(),
		TargetClusterName:             targetClusterName,
		TargetClusterDomain:           targetClusterDomain,
		TargetClusterLinkerdNamespace: targetClusterLinkerdNamespace,
		ClusterCredentialsSecret:      clusterCredentialsSecret,
		GatewayAddress:                gatewayAddress,
		GatewayPort:                   uint32(gatewayPort),
		GatewayIdentity:               gatewayIdentity,
		ProbeSpec:                     probeSpec,
		Selector:                      selector,
	}, nil
}

// ToUnstructured converts a Link struct into an unstructured resource that can
// be used by a kubernetes dynamic client.
func (l Link) ToUnstructured() (unstructured.Unstructured, error) {
	spec := map[string]interface{}{
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
	}

	data, err := json.Marshal(l.Selector)
	if err != nil {
		return unstructured.Unstructured{}, err
	}
	selector := make(map[string]interface{})
	err = json.Unmarshal(data, &selector)
	if err != nil {
		return unstructured.Unstructured{}, err
	}
	spec["selector"] = selector

	return unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": k8s.LinkAPIGroupVersion,
			"kind":       k8s.LinkKind,
			"metadata": map[string]interface{}{
				"name":      l.Name,
				"namespace": l.Namespace,
			},
			"spec": spec,
		},
	}, nil
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

// GetLinks fetches a list of all Link objects in the cluster.
func GetLinks(ctx context.Context, client dynamic.Interface) ([]Link, error) {
	list, err := client.Resource(LinkGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	links := []Link{}
	errs := []string{}
	for _, u := range list.Items {
		link, err := NewLink(u)
		if err != nil {
			errs = append(errs, fmt.Sprintf("failed to parse Link %s: %s", u.GetName(), err))
		} else {
			links = append(links, link)
		}
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "\n"))
	}
	return links, nil
}

// GetLink fetches a Link object from Kubernetes by name/namespace.
func GetLink(ctx context.Context, client dynamic.Interface, namespace, name string) (Link, error) {
	unstructured, err := client.Resource(LinkGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return Link{}, err
	}
	return NewLink(*unstructured)
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
