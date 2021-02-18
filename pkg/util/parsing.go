package util

import (
	"strconv"
	"strings"

	"github.com/linkerd/linkerd2-proxy-init/ports"
	"github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/prometheus/common/log"
	corev1 "k8s.io/api/core/v1"
)

// ParseContainerOpaquePorts parses the opaque ports annotation into a list of ports;
// this includes converting port ranges into separate ports and named ports
// into their port number equivalents.
func ParseContainerOpaquePorts(override string, containers []corev1.Container) []string {
	portRanges := GetPortRanges(override)
	var values []string
	for _, portRange := range portRanges {
		pr := portRange.GetPortRange()
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

// ParseOpaquePorts parses the opaque ports annotation into a list of ports
func ParseOpaquePorts(override string) []string {
	portRanges := GetPortRanges(override)
	var values []string
	for _, portRange := range portRanges {
		pr, err := ports.ParsePortRange(portRange.GetPortRange())
		if err != nil {
			log.Warnf("Invalid port range [%v]: %s", pr, err)
			continue
		}
		for i := pr.LowerBound; i <= pr.UpperBound; i++ {
			values = append(values, strconv.Itoa(i))
		}
	}
	return values
}

// GetPortRanges gets port ranges from an override annotation
func GetPortRanges(override string) []*config.PortRange {
	split := strings.Split(strings.TrimSuffix(override, ","), ",")
	ports := make([]*config.PortRange, len(split))
	for i, p := range split {
		ports[i] = &config.PortRange{PortRange: p}
	}
	return ports
}
