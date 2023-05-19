package util

import (
	"fmt"
	"strconv"
	"strings"
)

// PortRange defines the upper- and lower-bounds for a range of ports.
type PortRange struct {
	LowerBound int
	UpperBound int
}

// ParsePort parses and verifies the validity of the port candidate.
func ParsePort(port string) (int, error) {
	i, err := strconv.Atoi(port)
	if err != nil || !isPort(i) {
		return -1, fmt.Errorf("\"%s\" is not a valid TCP port", port)
	}
	return i, nil
}

// ParsePortRange parses and checks the provided range candidate to ensure it is
// a valid TCP port range.
func ParsePortRange(portRange string) (PortRange, error) {
	bounds := strings.Split(portRange, "-")
	if len(bounds) > 2 {
		return PortRange{}, fmt.Errorf("ranges expected as <lower>-<upper>")
	}
	if len(bounds) == 1 {
		// If only provided a single value, treat as both lower- and upper-bounds
		bounds = append(bounds, bounds[0])
	}
	lower, err := strconv.Atoi(bounds[0])
	if err != nil || !isPort(lower) {
		return PortRange{}, fmt.Errorf("\"%s\" is not a valid lower-bound", bounds[0])
	}
	upper, err := strconv.Atoi(bounds[1])
	if err != nil || !isPort(upper) {
		return PortRange{}, fmt.Errorf("\"%s\" is not a valid upper-bound", bounds[1])
	}
	if upper < lower {
		return PortRange{}, fmt.Errorf("\"%s\": upper-bound must be greater than or equal to lower-bound", portRange)
	}
	return PortRange{LowerBound: lower, UpperBound: upper}, nil
}

// isPort checks the provided to determine whether or not the port
// candidate is a valid TCP port number. Valid TCP ports range from 0 to 65535.
func isPort(port int) bool {
	return 0 <= port && port <= 65535
}

// Ports returns an array of all the ports contained by this range.
func (pr PortRange) Ports() []uint16 {
	var ports []uint16
	for i := pr.LowerBound; i <= pr.UpperBound; i++ {
		ports = append(ports, uint16(i))
	}
	return ports
}

func (pr PortRange) ToString() string {
	if pr.LowerBound == pr.UpperBound {
		return strconv.Itoa(pr.LowerBound)
	}

	return fmt.Sprintf("%d-%d", pr.LowerBound, pr.UpperBound)
}
