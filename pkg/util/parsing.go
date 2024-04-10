package util

import (
	"strings"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

// ParsePorts parses the given ports string into a map of ports;
// this includes converting port ranges into separate ports
func ParsePorts(portsString string) map[uint32]struct{} {
	opaquePorts := make(map[uint32]struct{})
	if portsString != "" {
		portRanges := GetPortRanges(portsString)
		for _, pr := range portRanges {
			portsRange, err := ParsePortRange(pr)
			if err != nil {
				log.Warnf("Invalid port range [%v]: %s", pr, err)
				continue
			}
			for i := portsRange.LowerBound; i <= portsRange.UpperBound; i++ {
				opaquePorts[uint32(i)] = struct{}{}
			}

		}
	}
	return opaquePorts
}

// ParseContainerOpaquePorts parses the opaque ports annotation into a list of
// port ranges, including validating port ranges and converting named ports
// into their port number equivalents.
func ParseContainerOpaquePorts(override string, namedPorts map[string]int32) []PortRange {
	portRanges := GetPortRanges(override)
	var values []PortRange
	for _, pr := range portRanges {
		port, named := namedPorts[pr]
		if named {
			values = append(values, PortRange{UpperBound: int(port), LowerBound: int(port)})
		} else {
			pr, err := ParsePortRange(pr)
			if err != nil {
				log.Warnf("Invalid port range [%v]: %s", pr, err)
				continue
			}
			values = append(values, pr)
		}
	}
	return values
}

func GetNamedPorts(containers []corev1.Container) map[string]int32 {
	namedPorts := make(map[string]int32)
	for _, container := range containers {
		for _, p := range container.Ports {
			if p.Name != "" {
				namedPorts[p.Name] = p.ContainerPort
			}
		}
	}

	return namedPorts
}

// GetPortRanges gets port ranges from an override annotation
func GetPortRanges(override string) []string {
	var ports []string
	for _, port := range strings.Split(strings.TrimSuffix(override, ","), ",") {
		ports = append(ports, strings.TrimSpace(port))
	}

	return ports
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
