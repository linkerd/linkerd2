package k8s

import (
	"fmt"
	"net"
	"testing"
)

func TestInitK8sProxy(t *testing.T) {
	t.Run("Returns an initialized Kubernetes Proxy object", func(t *testing.T) {
		kp, err := NewProxy("testdata/config.test", "", 0)
		if err != nil {
			t.Fatalf("Unexpected error creating Kubernetes API: %+v", err)
		}

		if kp.listener.Addr().Network() != "tcp" {
			t.Fatalf("Unexpected listener network: %+v", kp.listener.Addr().Network())
		}
	})
}

func TestKubernetesProxyUrlFor(t *testing.T) {
	const namespace = "some-namespace"
	const extraPath = "/some/extra/path"

	t.Run("Returns proxy URL based on the initialized KubernetesProxy", func(t *testing.T) {
		kp, err := NewProxy("testdata/config.test", "", 0)
		if err != nil {
			t.Fatalf("Unexpected error creating Kubernetes API: %+v", err)
		}
		actualURL, err := kp.URLFor(namespace, extraPath)
		if err != nil {
			t.Fatalf("Unexpected error generating URL: %+v", err)
		}

		url := actualURL.String()
		expected := fmt.Sprintf("http://127.0.0.1:%d/api/v1/namespaces/%s%s", kp.listener.Addr().(*net.TCPAddr).Port, namespace, extraPath)
		if url != expected {
			t.Fatalf("Expected generated URL to be [%s], but got [%s]", expected, url)
		}
	})
}

// TODO: test kb.Run()
