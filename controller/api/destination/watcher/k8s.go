package watcher

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/intstr"
)

type (
	// ID is a namespace-qualified name.
	ID struct {
		Namespace string
		Name      string
	}
	// ServiceID is the namespace-qualified name of a service.
	ServiceID = ID
	// PodID is the namespace-qualified name of a pod.
	PodID = ID
	// ProfileID is the namespace-qualified name of a service profile.
	ProfileID = ID

	// Port is a numeric port.
	Port      = uint32
	namedPort = intstr.IntOrString
)

func (i ID) String() string {
	return fmt.Sprintf("%s/%s", i.Namespace, i.Name)
}

func getHostAndPort(authority string) (string, Port, error) {
	hostPort := strings.Split(authority, ":")
	if len(hostPort) > 2 {
		return "", 0, fmt.Errorf("Invalid destination %s", authority)
	}
	host := hostPort[0]
	port := 80
	if len(hostPort) == 2 {
		var err error
		port, err = strconv.Atoi(hostPort[1])
		if err != nil {
			return "", 0, fmt.Errorf("Invalid port %s", hostPort[1])
		}
	}
	return host, Port(port), nil
}

// GetServiceAndPort is a utility function that destructures an authority into
// a service and port.  If the authority does not represent a Kubernetes
// service, an error is returned.  If no port is specified in the authority,
// the HTTP default (80) is returned as the port number.  If the authority
// is a pod DNS name then the pod hostname is also returned as the 3rd return
// value.  See https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/.
func GetServiceAndPort(authority string) (ServiceID, Port, string, error) {
	host, port, err := getHostAndPort(authority)
	if err != nil {
		return ServiceID{}, 0, "", err
	}
	domains := strings.Split(host, ".")
	suffix := []string{"svc", "cluster", "local"}
	n := len(domains)
	if n < 5 {
		return ServiceID{}, 0, "", fmt.Errorf("Invalid k8s service %s", host)
	}
	for i, subdomain := range domains[n-3:] {
		if subdomain != suffix[i] {
			return ServiceID{}, 0, "", fmt.Errorf("Invalid k8s service %s", host)
		}
	}
	if n == 5 {
		// <service>.<namespace>.svc.cluster.local
		service := ServiceID{
			Name:      domains[0],
			Namespace: domains[1],
		}
		return service, port, "", nil
	} else if n == 6 {
		// <hostname>.<service>.<namespace>.svc.cluster.local
		service := ServiceID{
			Name:      domains[1],
			Namespace: domains[2],
		}
		return service, port, domains[0], nil
	}
	return ServiceID{}, 0, "", fmt.Errorf("Invalid k8s service %s", host)
}
