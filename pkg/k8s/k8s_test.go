package k8s

import (
	"testing"
)

func TestGenerateKubernetesApiBaseUrlFor(t *testing.T) {
	t.Run("Generates correct URL when all elements are present", func(t *testing.T) {
		schemeHostAndPort := "ftp://some-server.example.com:666"
		namespace := "some-namespace"
		extraPath := "/starts/with/slash"
		url, err := generateKubernetesAPIBaseURLFor(schemeHostAndPort, namespace, extraPath)

		if err != nil {
			t.Fatalf("Unexpected error starting proxy: %v", err)
		}

		expectedURLString := "ftp://some-server.example.com:666/api/v1/namespaces/some-namespace/starts/with/slash"
		if url.String() != expectedURLString {
			t.Fatalf("Expected generated URl to be [%s], but got [%s]", expectedURLString, url.String())
		}
	})

	t.Run("Return error if extra path doesn't start with slash", func(t *testing.T) {
		schemeHostAndPort := "ftp://some-server.example.com:666"
		namespace := "some-namespace"
		extraPath := "does-not-start/with/slash"
		_, err := generateKubernetesAPIBaseURLFor(schemeHostAndPort, namespace, extraPath)

		if err == nil {
			t.Fatalf("Expected error when tryiong to generate URL with extra path without leading slash, got nothing")
		}
	})
}

func TestGenerateBaseKubernetesApiUrl(t *testing.T) {
	t.Run("Generates correct URL when all elements are present", func(t *testing.T) {
		schemeHostAndPort := "gopher://some-server.example.com:661"

		url, err := generateBaseKubernetesAPIURL(schemeHostAndPort)
		if err != nil {
			t.Fatalf("Unexpected error starting proxy: %v", err)
		}

		expectedURLString := "gopher://some-server.example.com:661/api/v1/"
		if url.String() != expectedURLString {
			t.Fatalf("Expected generated URl to be [%s], but got [%s]", expectedURLString, url.String())
		}
	})

	t.Run("Return error if invalid host and port", func(t *testing.T) {
		schemeHostAndPort := "ftp://some-server.exampl     e.com:666"
		_, err := generateBaseKubernetesAPIURL(schemeHostAndPort)

		if err == nil {
			t.Fatalf("Expected error when tryiong to generate URL with extra path without leading slash, got nothing")
		}
	})
}

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
			t.Fatalf("Expecting error when config file doesnt exist, got nothing")
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
