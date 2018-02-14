package k8s

import (
	"bufio"
	"bytes"
	"os"
	"strings"
	"testing"
)

var injectEnv = `
       - env:
         - name: CONDUIT_PROXY_LOG
           value: warn,conduit_proxy=info
         - name: CONDUIT_PROXY_CONTROL_URL
           value: tcp://proxy-api.conduit.svc.cluster.local:5555
         - name: CONDUIT_PROXY_CONTROL_LISTENER
           value: tcp://0.0.0.0:4190
         - name: CONDUIT_PROXY_PRIVATE_LISTENER
           value: tcp://127.0.0.1:4143
         - name: CONDUIT_PROXY_PUBLIC_LISTENER
           value: tcp://0.0.0.0:4140
         - name: CONDUIT_PROXY_NODE_NAME
`

func TestInjectYAML(t *testing.T) {
	t.Run("Run successful conduit inject on valid k8s yaml", func(t *testing.T) {
		file, err := os.Open("testdata/deployment.yml")
		if err != nil {
			t.Errorf("error opening test file: %v\n", err)
		}

		read := bufio.NewReader(file)

		output := new(bytes.Buffer)
		config := PodConfig{
			InitImage:             "conduit-init",
			ProxyImage:            "conduit-proxy",
			ProxyUID:              2102,
			InboundPort:           4140,
			OutboundPort:          4143,
			IgnoreInboundPorts:    []uint{3306},
			IgnoreOutboundPorts:   []uint{3306},
			ProxyControlPort:      4190,
			ProxyAPIPort:          5555,
			ProxyLogLevel:         "warn,conduit_proxy=info",
			ConduitVersion:        "v0.2.0",
			ImagePullPolicy:       "Always",
			ControlPlaneNamespace: "conduit",
		}

		InjectYAML(read, output, config)

		actualOutput := output.String()

		if strings.Contains(actualOutput, injectEnv) {
			t.Fatalf("actual yaml: %s\n does not contain injected proxy env:\n%s", actualOutput, injectEnv)
		}
	})
}
