package util

import (
	"fmt"
	"strconv"
	"strings"
)

// PortRange defines the upper- and lower-bounds for a range of ports.
type PortRange struct {
	LowerBound uint16
	UpperBound uint16
}

// ParsePort parses and verifies the validity of the port candidate.
func ParsePort(port string) (uint16, error) {
	// Ports are defined as 16-bit unsigned integers.
	i, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return 0, fmt.Errorf("\"%s\" is not a valid TCP port (must be a 16-bit unsigned integer)", port)
	}
	return uint16(i), nil
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
	lower, err := ParsePort(bounds[0])
	if err != nil {
		return PortRange{}, fmt.Errorf("\"%s\" is not a valid lower-bound (must be a 16-bit unsigned integer)", bounds[0])
	}
	upper, err := ParsePort(bounds[1])
	if err != nil {
		return PortRange{}, fmt.Errorf("\"%s\" is not a valid upper-bound: (must be a 16-bit unsigned integer)", bounds[1])
	}
	if upper < lower {
		return PortRange{}, fmt.Errorf("\"%s\": upper-bound must be greater than or equal to lower-bound", portRange)
	}
	return PortRange{LowerBound: lower, UpperBound: upper}, nil
}

// Ports returns an array of all the ports contained by this range.
func (pr PortRange) Ports() []uint16 {
	var ports []uint16
	for i := pr.LowerBound; i <= pr.UpperBound; i++ {
		ports = append(ports, i)
	}
	return ports
}

func (pr PortRange) ToString() string {
	if pr.LowerBound == pr.UpperBound {
		return fmt.Sprintf("%d", pr.LowerBound)
	}

	return fmt.Sprintf("%d-%d", pr.LowerBound, pr.UpperBound)
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
