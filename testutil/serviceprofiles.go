package testutil

import (
	"fmt"
	"strings"
	"time"

	cmd2 "github.com/linkerd/linkerd2/cli/cmd"
	"sigs.k8s.io/yaml"
)

// GetRoutes runs the `routes` cmd and returns a `JSONRouteStats` object
func GetRoutes(deployName, namespace string, additionalArgs []string, h *TestHelper) ([]*cmd2.JSONRouteStats, error) {
	cmd := []string{"routes", "--namespace", namespace, deployName}

	if len(additionalArgs) > 0 {
		cmd = append(cmd, additionalArgs...)
	}

	cmd = append(cmd, "--output", "json")
	var out, stderr string
	err := h.RetryFor(2*time.Minute, func() error {
		var err error
		out, stderr, err = h.LinkerdRun(cmd...)
		return err
	})
	if err != nil {
		return nil, err
	}

	var list map[string][]*cmd2.JSONRouteStats
	err = yaml.Unmarshal([]byte(out), &list)
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("Error: %s stderr: %s", err, stderr))
	}

	if deployment, ok := list[deployName]; ok {
		return deployment, nil
	}
	return nil, fmt.Errorf("could not retrieve route info for %s", deployName)
}

// AssertExpectedRoutes matches expected routes by matching them against given routes
func AssertExpectedRoutes(expected []string, actual []*cmd2.JSONRouteStats) error {

	if len(expected) != len(actual) {
		return fmt.Errorf("mismatch routes count. Expected %d, Actual %d", len(expected), len(actual))
	}

	for _, expectedRoute := range expected {
		containsRoute := false
		for _, actualRoute := range actual {
			if actualRoute.Route == expectedRoute {
				containsRoute = true
				break
			}
		}
		if !containsRoute {
			sb := strings.Builder{}
			for _, route := range actual {
				sb.WriteString(fmt.Sprintf("%s ", route.Route))
			}
			return fmt.Errorf("expected route %s not found in %+v", expectedRoute, sb.String())
		}
	}
	return nil
}
