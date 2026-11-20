package k8s

import (
	"fmt"
	"os"
	"path/filepath"
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

func TestRESTConfigFromRemoteKubeConfig(t *testing.T) {
	kubeconfig := func(cluster, user string) []byte {
		return []byte(fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: remote
  cluster:
    server: https://remote.example.com
%s
users:
- name: remote
  user:
%s
contexts:
- name: remote
  context:
    cluster: remote
    user: remote
current-context: remote
`, cluster, user))
	}

	t.Run("Accepts inlined credentials", func(t *testing.T) {
		config, err := RESTConfigFromRemoteKubeConfig(kubeconfig("", "    token: remote-token"))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if config.Host != "https://remote.example.com" {
			t.Fatalf("Expected host to be [https://remote.example.com] got [%s]", config.Host)
		}
		if config.BearerToken != "remote-token" {
			t.Fatalf("Expected bearer token to be [remote-token] got [%s]", config.BearerToken)
		}
	})

	// The referenced files must exist, otherwise client-go fails to load
	// the kubeconfig on its own.
	localFile := filepath.Join(t.TempDir(), "local")
	if err := os.WriteFile(localFile, []byte("local"), 0o600); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	rejected := map[string][]byte{
		"token file":                 kubeconfig("", "    tokenFile: "+localFile),
		"client certificate file":    kubeconfig("", "    client-certificate: "+localFile+"\n    client-key: "+localFile),
		"certificate authority file": kubeconfig("    certificate-authority: "+localFile, "    token: remote-token"),
		"exec plugin":                kubeconfig("", "    exec:\n      apiVersion: client.authentication.k8s.io/v1\n      command: /bin/sh\n      interactiveMode: Never"),
		"auth-provider plugin":       kubeconfig("", "    auth-provider:\n      name: oidc"),
	}
	for name, data := range rejected {
		t.Run(fmt.Sprintf("Rejects %s", name), func(t *testing.T) {
			if _, err := RESTConfigFromRemoteKubeConfig(data); err == nil {
				t.Fatalf("Expected error for kubeconfig with %s, got nothing", name)
			}
		})
	}
}

func TestCanonicalResourceNameFromFriendlyName(t *testing.T) {
	t.Run("Returns canonical name for all known variants", func(t *testing.T) {
		expectations := map[string]string{
			"po":           Pod,
			"pod":          Pod,
			"deployment":   Deployment,
			"deployments":  Deployment,
			"cj":           CronJob,
			"cronjob":      CronJob,
			"serverauthz":  ServerAuthorization,
			"srvauthz":     ServerAuthorization,
			"authzpolicy":  AuthorizationPolicy,
			"meshtlsauthn": MeshTLSAuthentication,
			"networkauthn": NetworkAuthentication,
			"netauthn":     NetworkAuthentication,
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
