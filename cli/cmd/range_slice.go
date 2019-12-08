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
			lower, err := strconv.ParseUint(bounds[0], 10, 0)
			if err != nil {
				return fmt.Errorf("\"%s\" is not a valid lower-bound", bounds[0])
			}
			upper, err := strconv.ParseUint(bounds[1], 10, 0)
			if err != nil {
				return fmt.Errorf("\"%s\" is not a valid upper-bound", bounds[1])
			}
			if upper < lower {
				return fmt.Errorf("\"%s\": upper-bound must be greater than or equal to lower-bound", portOrRange)
			}
		} else {
			_, err := strconv.ParseUint(portOrRange, 10, 0)
			if err != nil {
				return fmt.Errorf("\"%s\" is not a valid port nor port-range", portOrRange)
			}
		}
	}
	return nil
}
