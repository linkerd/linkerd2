package k8s

import (
	"path/filepath"
	"testing"
)

func TestGenerateKubernetesApiBaseUrlFor(t *testing.T) {
	t.Run("Generates correct URL when all elements are present", func(t *testing.T) {
		schemeHostAndPort := "ftp://some-server.example.com:666"
		namespace := "some-namespace"
		extraPath := "/starts/with/slash"
		url, err := generateKubernetesApiBaseUrlFor(schemeHostAndPort, namespace, extraPath)

		if err != nil {
			t.Fatalf("Unexpected error starting proxy: %v", err)
		}

		expectedUrlString := "ftp://some-server.example.com:666/api/v1/namespaces/some-namespace/starts/with/slash"
		if url.String() != expectedUrlString {
			t.Fatalf("Expected generated URl to be [%s], but got [%s]", expectedUrlString, url.String())
		}
	})

	t.Run("Return error it extra path doesn't start with slash", func(t *testing.T) {
		schemeHostAndPort := "ftp://some-server.example.com:666"
		namespace := "some-namespace"
		extraPath := "does-not-start/with/slash"
		_, err := generateKubernetesApiBaseUrlFor(schemeHostAndPort, namespace, extraPath)

		if err == nil {
			t.Fatalf("Expected error when tryiong to generate URL with extra path without leading slash, got nothing")
		}
	})
}

func TestParseK8SConfig(t *testing.T) {
	t.Run("Gets host correctly form existing file", func(t *testing.T) {
		config, err := parseK8SConfig("testdata/config.test")
		if err != nil {
			t.Fatalf("Unexpected error starting proxy: %v", err)
		}

		expectedHost := "https://55.197.171.239"
		if config.Host != expectedHost {
			t.Fatalf("Expected host to be [%s] got [%s]", expectedHost, config.Host)
		}
	})

	t.Run("Returns error if configuration cannot be found", func(t *testing.T) {
		_, err := parseK8SConfig("/this/doest./not/exist.config")
		if err == nil {
			t.Fatalf("Expecting error when config file doesnt exist, got nothing")
		}
	})
}

func TestFindK8sConfigFile(t *testing.T) {
	override := "/this/is/overrriden"
	envVarContents := "~/tmp/.kube"
	homeDir := "/home/bob"

	t.Run("When override is set, everything else is ignored", func(t *testing.T) {
		whereTheConfigFileIs := findK8sConfigFile(override, envVarContents, homeDir)

		if whereTheConfigFileIs != override {
			t.Fatalf("Expected override [%s] to take precedence, but it was [%s]", override, whereTheConfigFileIs)
		}
	})

	t.Run("When override NOT set, $KUBECONFIG takes precedence", func(t *testing.T) {
		whereTheConfigFileIs := findK8sConfigFile("", envVarContents, homeDir)

		if whereTheConfigFileIs != envVarContents {
			t.Fatalf("Expected $KUBECONFIG [%s] to take precedence, but it was [%s]", envVarContents, envVarContents)
		}
	})

	t.Run("When override NOT set, and $KUBECONFIG is NOT set, takes default dir", func(t *testing.T) {
		whereTheConfigFileIs := findK8sConfigFile("", "", homeDir)

		expectedDir := filepath.Join(homeDir, ".kube", "config")
		if whereTheConfigFileIs != expectedDir {
			t.Fatalf("Expected default directory [%s] to take precedence, but it was [%s]", expectedDir, expectedDir)
		}
	})
}
