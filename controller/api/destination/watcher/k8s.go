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
// the HTTP default (80) is returned as the port number.
func GetServiceAndPort(authority string) (ServiceID, Port, error) {
	host, port, err := getHostAndPort(authority)
	if err != nil {
		return ServiceID{}, 0, err
	}
	domains := strings.Split(host, ".")
	// S.N.svc.cluster.local
	if len(domains) != 5 {
		return ServiceID{}, 0, fmt.Errorf("Invalid k8s service %s", host)
	}
	suffix := []string{"svc", "cluster", "local"}
	for i, subdomain := range domains[2:] {
		if subdomain != suffix[i] {
			return ServiceID{}, 0, fmt.Errorf("Invalid k8s service %s", host)
		}
	}
	service := ServiceID{
		Name:      domains[0],
		Namespace: domains[1],
	}
	return service, port, nil
}
