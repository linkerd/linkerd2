package cmd

import (
	"fmt"
	"strconv"
	"strings"
)

// validateRangeSlice ensures that provided slice contains valid entries
// representing either port number(s) and/or port range(s).  Invalid entries
// will result in an error being returned.
func validateRangeSlice(rangeSlice []string) error {
	for _, portOrRange := range rangeSlice {
		if strings.Contains(portOrRange, "-") {
			bounds := strings.Split(portOrRange, "-")
			if len(bounds) != 2 {
				return fmt.Errorf("ranges expected as <lower>-<upper>")
			}
			lower, err := strconv.Atoi(bounds[0])
			if err != nil || !isValidPort(lower) {
				return fmt.Errorf("\"%s\" is not a valid lower-bound", bounds[0])
			}
			upper, err := strconv.Atoi(bounds[1])
			if err != nil || !isValidPort(upper) {
				return fmt.Errorf("\"%s\" is not a valid upper-bound", bounds[1])
			}
			if upper < lower {
				return fmt.Errorf("\"%s\": upper-bound must be greater than or equal to lower-bound", portOrRange)
			}
		} else {
			port, err := strconv.Atoi(portOrRange)
			if err != nil || !isValidPort(port) {
				return fmt.Errorf("\"%s\" is not a valid port nor port-range", portOrRange)
			}
		}
	}
	return nil
}

// isValidPort ensures that the provided value is a valid TCP port number, 0-65535 (inclusive).
func isValidPort(port int) bool {
	return port >= 0 && port <= 65535
}
