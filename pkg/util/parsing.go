package util

import (
	"strconv"
	"strings"

	"github.com/linkerd/linkerd2-proxy-init/ports"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

// ParsePorts parses the given ports string into a map of ports;
// this includes converting port ranges into separate ports
func ParsePorts(portsString string) (map[uint32]struct{}, error) {
	opaquePorts := make(map[uint32]struct{})
	if portsString != "" {
		portRanges := GetPortRanges(portsString)
		for _, pr := range portRanges {
			portsRange, err := ports.ParsePortRange(pr)
			if err != nil {
				log.Warnf("Invalid port range [%v]: %s", pr, err)
				continue
			}
			for i := portsRange.LowerBound; i <= portsRange.UpperBound; i++ {
				opaquePorts[uint32(i)] = struct{}{}
			}

		}
	}
	return opaquePorts, nil
}

// ParseContainerOpaquePorts parses the opaque ports annotation into a list of ports;
// this includes converting port ranges into separate ports and named ports
// into their port number equivalents.
func ParseContainerOpaquePorts(override string, containers []corev1.Container) []string {
	portRanges := GetPortRanges(override)
	var values []string
	for _, pr := range portRanges {
		port, named := isNamed(pr, containers)
		if named {
			values = append(values, strconv.Itoa(int(port)))
		} else {
			pr, err := ports.ParsePortRange(pr)
			if err != nil {
				log.Warnf("Invalid port range [%v]: %s", pr, err)
				continue
			}
			for i := pr.LowerBound; i <= pr.UpperBound; i++ {
				values = append(values, strconv.Itoa(i))
			}
		}
	}
	return values
}

// GetPortRanges gets port ranges from an override annotation
func GetPortRanges(override string) []string {
	var ports []string
	for _, port := range strings.Split(strings.TrimSuffix(override, ","), ",") {
		ports = append(ports, strings.TrimSpace(port))
	}

	return ports
}

// isNamed checks if a port range is actually a container named port (e.g.
// `123-456` is a valid name, but also is a valid range); all port names must
// be checked before making it a list.
func isNamed(pr string, containers []corev1.Container) (int32, bool) {
	for _, c := range containers {
		for _, p := range c.Ports {
			if p.Name == pr {
				return p.ContainerPort, true
			}
		}
	}
	return 0, false
}

// ContainsString checks if a string collections contains the given string.
func ContainsString(str string, collection []string) bool {
	for _, e := range collection {
		if str == e {
			return true
		}
	}
	return false
}
