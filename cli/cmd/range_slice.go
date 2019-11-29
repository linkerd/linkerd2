package cmd

import (
	"fmt"
	"strconv"
	"strings"
)

// MapRangeSlice creates and populates the target slice of unsigned integers
// based upon the source slice of string integer(s) and inclusive range(s).
// For example:
//   []string{"23","25-27"}
// will result in
//   []uint{23,25,26,27}
func MapRangeSlice(target *[]uint, source []string) error {
	*target = make([]uint, 0)
	for _, portOrRange := range source {
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
			for i := lower; i <= upper; i++ {
				*target = append(*target, uint(i))
			}
		} else {
			u, err := strconv.ParseUint(portOrRange, 10, 0)
			if err != nil {
				return err
			}
			*target = append(*target, uint(u))
		}
	}
	return nil
}
