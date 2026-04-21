package k8s

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var revisionSeparator = regexp.MustCompile("[^0-9.]")

func getK8sVersion(versionString string) ([3]int, error) {
	var version [3]int
	justTheVersionString := strings.TrimPrefix(versionString, "v")
	justTheMajorMinorRevisionNumbers := revisionSeparator.Split(justTheVersionString, -1)[0]
	split := strings.Split(justTheMajorMinorRevisionNumbers, ".")

	if len(split) < 3 {
		return version, fmt.Errorf("unknown version string format [%s]", versionString)
	}

	for i, segment := range split {
		v, err := strconv.Atoi(strings.TrimSpace(segment))
		if err != nil {
			return version, fmt.Errorf("unknown version string format [%s]", versionString)
		}
		version[i] = v
	}

	return version, nil
}

func isCompatibleVersion(minimalRequirementVersion [3]int, actualVersion [3]int) bool {
	if minimalRequirementVersion[0] < actualVersion[0] {
		return true
	}

	if (minimalRequirementVersion[0] == actualVersion[0]) && minimalRequirementVersion[1] < actualVersion[1] {
		return true
	}

	if (minimalRequirementVersion[0] == actualVersion[0]) && (minimalRequirementVersion[1] == actualVersion[1]) && (minimalRequirementVersion[2] <= actualVersion[2]) {
		return true
	}

	return false
}
