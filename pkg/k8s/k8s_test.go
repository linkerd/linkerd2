package k8s

import (
	"testing"
)

func TestGetConfig(t *testing.T) {
	t.Run("Gets host correctly form existing file", func(t *testing.T) {
		config, err := GetConfig("testdata/config.test", "")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedHost := "https://55.197.171.239"
		if config.Host != expectedHost {
			t.Fatalf("Expected host to be [%s] got [%s]", expectedHost, config.Host)
		}
	})

	t.Run("Returns error if configuration cannot be found", func(t *testing.T) {
		_, err := GetConfig("/this/doest./not/exist.config", "")
		if err == nil {
			t.Fatalf("Expecting error when config file does not exist, got nothing")
		}
	})
}

func TestCanonicalResourceNameFromFriendlyName(t *testing.T) {
	t.Run("Returns canonical name for all known variants", func(t *testing.T) {
		expectations := map[string]string{
			"po":          Pod,
			"pod":         Pod,
			"deployment":  Deployment,
			"deployments": Deployment,
			"au":          Authority,
			"authorities": Authority,
			"cj":          CronJob,
			"cronjob":     CronJob,
		}

		for input, expectedName := range expectations {
			actualName, err := CanonicalResourceNameFromFriendlyName(input)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if actualName != expectedName {
				t.Fatalf("Expected friendly name [%s] to resolve to [%s], but got [%s]", input, expectedName, actualName)
			}
		}
	})

	t.Run("Returns error if input isn't a supported name", func(t *testing.T) {
		unsupportedNames := []string{
			"pdo", "dop", "paths", "path", "", "mesh",
		}

		for _, n := range unsupportedNames {
			out, err := CanonicalResourceNameFromFriendlyName(n)
			if err == nil {
				t.Fatalf("Expecting error when resolving [%s], but it did resolve to [%s]", n, out)
			}
		}
	})
}
