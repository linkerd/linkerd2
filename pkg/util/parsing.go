package util

import (
	"strconv"
	"strings"

	"github.com/linkerd/linkerd2-proxy-init/ports"
	"github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/prometheus/common/log"
	corev1 "k8s.io/api/core/v1"
)

// ParseOpaquePorts parses the opaque ports annotation into a list of ports;
// this includes converting port ranges into separate ports and named ports
// into their port number equivalents.
func ParseOpaquePorts(override string, containers []corev1.Container) []string {
	var portRanges []*config.PortRange
	split := strings.Split(strings.TrimSuffix(override, ","), ",")
	portRanges = ToPortRanges(split)

	var values []string
	for _, portRange := range portRanges {
		pr := portRange.GetPortRange()

		// It is valid for the format of a port name to be the same as a
		// port range (e.g. `123-456` is a valid name, but also is a valid
		// range). All port names must be checked before making it a list.
		named := false
		for _, c := range containers {
			for _, p := range c.Ports {
				if p.Name == pr {
					named = true
					values = append(values, strconv.Itoa(int(p.ContainerPort)))
				}
			}
		}

		if !named {
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

// ToPortRanges converts a slice of strings into a slice of PortRanges.
func ToPortRanges(portRanges []string) []*config.PortRange {
	ports := make([]*config.PortRange, len(portRanges))
	for i, p := range portRanges {
		ports[i] = &config.PortRange{PortRange: p}
	}
	return ports
}
