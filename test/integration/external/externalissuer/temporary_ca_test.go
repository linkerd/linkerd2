package externalissuer

import (
	"bytes"
	"encoding/base64"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/testutil"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

// TestTemporaryCAHelmTemplate ensures that enabling identity.temporaryCA renders the
// same external CA resources that the integration tests expect to manage manually.
func TestTemporaryCAHelmTemplate(t *testing.T) {
	chartPath := filepath.Join(TestHelper.GetHelmCharts(), "linkerd-control-plane")
	stdout, stderr, err := TestHelper.HelmRun(
		"template",
		"tempca",
		chartPath,
		"--namespace", TestHelper.GetLinkerdNamespace(),
		"--set", "identity.temporaryCA=24h",
	)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'helm template' command failed", "'helm template' command failed: %s\n%s", err, stderr)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("'helm template' emitted stderr: %s", stderr)
	}

	var (
		secretFound    bool
		configMapFound bool
	)

	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewBufferString(stdout), 4096)

	for {
		var doc map[string]any
		if err := decoder.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			testutil.AnnotatedFatalf(t, "failed to decode manifest", "failed to decode manifest: %s", err)
		}

		if len(doc) == 0 {
			continue
		}

		kind, _ := doc["kind"].(string)
		meta, _ := doc["metadata"].(map[string]any)
		name, _ := meta["name"].(string)

		switch {
		case kind == "Secret" && name == "linkerd-identity-issuer":
			secretFound = true

			secretType, _ := doc["type"].(string)
			if secretType != "kubernetes.io/tls" {
				t.Fatalf("expected linkerd-identity-issuer secret type kubernetes.io/tls, got %q", secretType)
			}

			data, ok := doc["data"].(map[string]any)
			if !ok {
				t.Fatalf("expected linkerd-identity-issuer secret to contain data map")
			}

			for _, key := range []string{"tls.crt", "tls.key", "ca.crt"} {
				value, present := data[key].(string)
				if !present {
					t.Fatalf("expected linkerd-identity-issuer secret to contain %s", key)
				}
				decoded, err := base64.StdEncoding.DecodeString(value)
				if err != nil {
					t.Fatalf("failed to base64 decode %s: %s", key, err)
				}
				if len(decoded) == 0 {
					t.Fatalf("expected %s to be non-empty", key)
				}
			}

			ca, _ := data["ca.crt"].(string)
			crt, _ := data["tls.crt"].(string)
			if ca != crt {
				t.Fatalf("expected ca.crt to match tls.crt for linkerd-identity-issuer secret")
			}

		case kind == "ConfigMap" && name == "linkerd-identity-trust-roots":
			configMapFound = true

			data, ok := doc["data"].(map[string]any)
			if !ok {
				t.Fatalf("expected linkerd-identity-trust-roots configmap to contain data map")
			}

			value, present := data["ca-bundle.crt"].(string)
			if !present {
				t.Fatalf("expected linkerd-identity-trust-roots configmap to contain ca-bundle.crt")
			}
			if strings.TrimSpace(value) == "" {
				t.Fatalf("expected ca-bundle.crt to be non-empty")
			}
			if !strings.Contains(value, "BEGIN CERTIFICATE") {
				t.Fatalf("expected ca-bundle.crt to contain a PEM certificate, got %q", value)
			}

		default:
			continue
		}

		if secretFound && configMapFound {
			break
		}
	}

	if !secretFound {
		t.Fatal("did not find rendered linkerd-identity-issuer secret")
	}
	if !configMapFound {
		t.Fatal("did not find rendered linkerd-identity-trust-roots configmap")
	}

	// Ensure no legacy secret containing crt.pem/key.pem was rendered.
	if strings.Contains(stdout, "crt.pem:") {
		t.Fatalf("unexpected legacy credential key crt.pem present in temporary CA rendering for %s", k8s.IdentityIssuerSecretName)
	}
}
