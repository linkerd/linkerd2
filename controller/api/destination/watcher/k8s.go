package watcher

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/intstr"
)

type (
	iD struct {
		Namespace string
		Name      string
	}
	ServiceID = iD
	PodID     = iD
	ProfileID = iD

	Port      = uint32
	namedPort = intstr.IntOrString
)

func (i iD) String() string {
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
