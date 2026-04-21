package testutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	vizHealthcheck "github.com/linkerd/linkerd2/viz/pkg/healthcheck"
)

var preCategories = []healthcheck.CategoryID{
	healthcheck.KubernetesAPIChecks,
	healthcheck.KubernetesVersionChecks,
	healthcheck.LinkerdPreInstallChecks,
	healthcheck.LinkerdVersionChecks,
}

var coreCategories = []healthcheck.CategoryID{
	healthcheck.KubernetesAPIChecks,
	healthcheck.KubernetesVersionChecks,
	healthcheck.LinkerdControlPlaneExistenceChecks,
	healthcheck.LinkerdConfigChecks,
	healthcheck.LinkerdIdentity,
	healthcheck.LinkerdWebhooksAndAPISvcTLS,
	healthcheck.LinkerdVersionChecks,
	healthcheck.LinkerdControlPlaneProxyChecks,
}

var dataPlaneCategories = []healthcheck.CategoryID{
	healthcheck.LinkerdIdentityDataPlane,
	healthcheck.LinkerdControlPlaneProxyChecks,
	healthcheck.LinkerdDataPlaneChecks,
}

// TestCheckPre runs validates the output of `linkerd check --pre`
func (h *TestHelper) TestCheckPre() error {
	cmd := []string{"check", "--pre", "--output", "json", "--wait", "5m"}
	return h.testCheck(cmd, preCategories)
}

// TestCheck runs validates the output of `linkerd check`
func (h *TestHelper) TestCheck(extraArgs ...string) error {
	return h.TestCheckWith([]healthcheck.CategoryID{healthcheck.LinkerdControlPlaneVersionChecks, vizHealthcheck.LinkerdVizExtensionCheck}, extraArgs...)
}

// TestCheckWith validates the output of `linkerd check`. It will validate the
// core categories and any additional categories that the caller provides.
func (h *TestHelper) TestCheckWith(additional []healthcheck.CategoryID, extraArgs ...string) error {
	cmd := []string{"check", "--output", "json", "--wait", "5m"}
	cmd = append(cmd, extraArgs...)
	categories := append(coreCategories, additional...)
	return h.testCheck(cmd, categories)
}

// TestCheckProxy runs validates the output of `linkerd check --proxy`
func (h *TestHelper) TestCheckProxy(expectedVersion, namespace string) error {
	cmd := []string{"check", "--proxy", "--expected-version", expectedVersion,
		"--namespace", namespace, "--output", "json", "--wait", "5m"}
	categories := append(coreCategories, vizHealthcheck.LinkerdVizExtensionCheck,
		vizHealthcheck.LinkerdVizExtensionDataPlaneCheck)
	categories = append(categories, dataPlaneCategories...)
	return h.testCheck(cmd, categories)
}

func (h *TestHelper) testCheck(cmd []string, categories []healthcheck.CategoryID) error {
	timeout := time.Minute * 10
	return RetryFor(timeout, func() error {
		res, err := h.LinkerdRun(cmd...)
		if err != nil {
			return fmt.Errorf("'linkerd check' command failed\n%w\n%s", err, res)
		}

		returnedCats := map[healthcheck.CategoryID]struct{}{}

		// We can't just use json.Unmarshal() because the check output is formatted as NDJSON
		d := json.NewDecoder(strings.NewReader(res))
		for {
			var out healthcheck.CheckOutput
			err := d.Decode(&out)
			if err != nil {
				// io.EOF is expected at end of stream.
				if !errors.Is(err, io.EOF) {
					return fmt.Errorf("error processing 'linkerd check' output: %w", err)
				}
				break
			}

			errs := []string{}
			for _, cat := range out.Categories {
				for _, check := range cat.Checks {
					returnedCats[cat.Name] = struct{}{}
					if check.Result == healthcheck.CheckErr {
						errs = append(errs, fmt.Sprintf("%s: %s", cat.Name, check.Error))
					}
				}
			}
			if len(errs) > 0 {
				return errors.New(strings.Join(errs, "\n"))
			}
		}

		errs := []string{}
		for _, cat := range categories {
			if _, ok := returnedCats[cat]; !ok {
				errs = append(errs, fmt.Sprintf("missing category '%s'", cat))
			}
		}
		if len(errs) > 0 {
			return errors.New(strings.Join(errs, "\n"))
		}

		return nil
	})
}
