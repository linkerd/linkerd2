package servicemirror

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func diffServices(expected, actual *corev1.Service) error {
	if expected.Name != actual.Name {
		return fmt.Errorf("was expecting service with name %s but was %s", expected.Name, actual.Name)
	}

	if expected.Namespace != actual.Namespace {
		return fmt.Errorf("was expecting service with namespace %s but was %s", expected.Namespace, actual.Namespace)
	}

	if !reflect.DeepEqual(expected.Annotations, actual.Annotations) {
		return fmt.Errorf("was expecting service with annotations %v but got %v", expected.Annotations, actual.Annotations)
	}

	if !reflect.DeepEqual(expected.Labels, actual.Labels) {
		return fmt.Errorf("was expecting service with labels %v but got %v", expected.Labels, actual.Labels)
	}

	return nil
}

func diffEndpoints(expected, actual *corev1.Endpoints) error {
	if expected.Name != actual.Name {
		return fmt.Errorf("was expecting endpoints with name %s but was %s", expected.Name, actual.Name)
	}

	if expected.Namespace != actual.Namespace {
		return fmt.Errorf("was expecting endpoints with namespace %s but was %s", expected.Namespace, actual.Namespace)
	}

	if !reflect.DeepEqual(expected.Annotations, actual.Annotations) {
		return fmt.Errorf("was expecting endpoints with annotations %v but got %v", expected.Annotations, actual.Annotations)
	}

	if !reflect.DeepEqual(expected.Labels, actual.Labels) {
		return fmt.Errorf("was expecting endpoints with labels %v but got %v", expected.Labels, actual.Labels)
	}

	if !reflect.DeepEqual(expected.Subsets, actual.Subsets) {
		return fmt.Errorf("was expecting endpoints with subsets %v but got %v", expected.Subsets, actual.Subsets)
	}

	return nil
}

func remoteService(name, namespace, gtwName, gtwNs, resourceVersion string, ports []corev1.ServicePort) *corev1.Service {
	annotations := make(map[string]string)
	if gtwName != "" && gtwNs != "" {
		annotations[consts.GatewayNameAnnotation] = gtwName
		annotations[consts.GatewayNsAnnotation] = gtwNs
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: resourceVersion,
			Annotations:     annotations,
		},
		Spec: corev1.ServiceSpec{
			Ports: ports,
		},
	}
}

func remoteServiceAsYaml(name, namespace, gtwName, gtwNs, resourceVersion string, ports []corev1.ServicePort, t *testing.T) string {
	svc := remoteService(name, namespace, gtwName, gtwNs, resourceVersion, ports)

	bytes, err := yaml.Marshal(svc)
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes)
}

func mirroredService(name, namespace, gtwName, gtwNs, resourceVersion, gatewayResourceVersion string, ports []corev1.ServicePort) *corev1.Service {
	annotations := make(map[string]string)
	annotations[consts.RemoteResourceVersionAnnotation] = resourceVersion
	annotations[consts.RemoteServiceFqName] = fmt.Sprintf("%s.%s.svc.cluster.local", strings.Replace(name, "-remote", "", 1), namespace)

	if gatewayResourceVersion != "" {
		annotations[consts.RemoteGatewayResourceVersionAnnotation] = gatewayResourceVersion

	}
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				consts.RemoteClusterNameLabel: "remote",
				consts.MirroredResourceLabel:  "true",
				consts.RemoteGatewayNameLabel: gtwName,
				consts.RemoteGatewayNsLabel:   gtwNs,
			},
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Ports: ports,
		},
	}
}

func mirroredServiceAsYaml(name, namespace, gtwName, gtwNs, resourceVersion, gatewayResourceVersion string, ports []corev1.ServicePort, t *testing.T) string {
	svc := mirroredService(name, namespace, gtwName, gtwNs, resourceVersion, gatewayResourceVersion, ports)

	bytes, err := yaml.Marshal(svc)
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes)
}

func gateway(name, namespace, resourceVersion, ip, portName string, port int32, identity string) *corev1.Service {
	svc := corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: resourceVersion,
			Annotations: map[string]string{
				consts.GatewayIdentity: identity,
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     portName,
					Protocol: "TCP",
					Port:     port,
				},
			},
		},
	}

	if ip != "" {
		svc.Status.LoadBalancer.Ingress = append(svc.Status.LoadBalancer.Ingress, corev1.LoadBalancerIngress{IP: ip})
	}
	return &svc
}

func gatewayAsYaml(name, namespace, resourceVersion, ip, portName string, port int32, identity string, t *testing.T) string {
	gtw := gateway(name, namespace, resourceVersion, ip, portName, port, identity)

	bytes, err := yaml.Marshal(gtw)
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes)
}

func endpoints(name, namespace, gtwName, gtwNs, gatewayIP string, gatewayIdentity string, ports []corev1.EndpointPort) *corev1.Endpoints {
	var subsets []corev1.EndpointSubset
	if gatewayIP != "" {
		subsets = []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						IP: gatewayIP,
					},
				},
				Ports: ports,
			},
		}
	}

	endpoints := &corev1.Endpoints{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Endpoints",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				consts.RemoteClusterNameLabel: "remote",
				consts.MirroredResourceLabel:  "true",
				consts.RemoteGatewayNameLabel: gtwName,
				consts.RemoteGatewayNsLabel:   gtwNs,
			},
			Annotations: map[string]string{
				consts.RemoteServiceFqName: fmt.Sprintf("%s.%s.svc.cluster.local", strings.Replace(name, "-remote", "", 1), namespace),
			},
		},
		Subsets: subsets,
	}

	if gatewayIdentity != "" {
		endpoints.Annotations[consts.RemoteGatewayIdentity] = gatewayIdentity
	}

	return endpoints
}

func endpointsAsYaml(name, namespace, gtwName, gtwNs, gatewayIP, gatewayIdentity string, ports []corev1.EndpointPort, t *testing.T) string {
	ep := endpoints(name, namespace, gtwName, gtwNs, gatewayIP, gatewayIdentity, ports)

	bytes, err := yaml.Marshal(ep)
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes)
}
