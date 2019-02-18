package cmd

import (
	"bytes"
	"fmt"
	"testing"
)

func TestRenderCNIPlugin(t *testing.T) {
	defaultControlPlaneNamespace := controlPlaneNamespace
	defaultOptions := newCNIPluginOptions()
	defaultConfig, err := validateAndBuildCNIConfig(defaultOptions)
	if err != nil {
		t.Fatalf("Unexpected error from validateAndBuildCNIConfig(): %v", err)
	}

	fullyConfiguredOptions := cniPluginOptions{
		linkerdVersion:      "awesome-linkerd-version.1",
		dockerRegistry:      "gcr.io/linkerd-io",
		proxyControlPort:    5190,
		proxyMetricsPort:    5191,
		inboundPort:         5143,
		outboundPort:        5140,
		ignoreInboundPorts:  make([]uint, 0),
		ignoreOutboundPorts: make([]uint, 0),
		proxyUID:            12102,
		cniPluginImage:      "my-docker-registry.io/awesome/cni-plugin-test-image",
		logLevel:            "debug",
		destCNINetDir:       "/etc/kubernetes/cni/net.d",
		destCNIBinDir:       "/opt/my-cni/bin",
	}
	fullyConfiguredConfig, err := validateAndBuildCNIConfig(&fullyConfiguredOptions)
	if err != nil {
		t.Fatalf("Unexpected error from validateAndBuildCNIConfig(): %v", err)
	}
	fullyConfiguredConfig.Namespace = "other"

	fullyConfiguredOptionsEqualDsts := cniPluginOptions{
		linkerdVersion:      "awesome-linkerd-version.1",
		dockerRegistry:      "gcr.io/linkerd-io",
		proxyControlPort:    5190,
		proxyMetricsPort:    5191,
		inboundPort:         5143,
		outboundPort:        5140,
		ignoreInboundPorts:  make([]uint, 0),
		ignoreOutboundPorts: make([]uint, 0),
		proxyUID:            12102,
		cniPluginImage:      "my-docker-registry.io/awesome/cni-plugin-test-image",
		logLevel:            "debug",
		destCNINetDir:       "/etc/kubernetes/cni/net.d",
		destCNIBinDir:       "/etc/kubernetes/cni/net.d",
	}
	fullyConfiguredConfigEqualDsts, err := validateAndBuildCNIConfig(&fullyConfiguredOptionsEqualDsts)
	if err != nil {
		t.Fatalf("Unexpected error from validateAndBuildCNIConfig(): %v", err)
	}
	fullyConfiguredConfigEqualDsts.Namespace = "other"

	testCases := []struct {
		*installCNIPluginConfig
		namespace      string
		goldenFileName string
	}{
		{defaultConfig, defaultControlPlaneNamespace, "install-cni-plugin_default.golden"},
		{fullyConfiguredConfig, fullyConfiguredConfig.Namespace, "install-cni-plugin_fully_configured.golden"},
		{fullyConfiguredConfigEqualDsts, fullyConfiguredConfigEqualDsts.Namespace, "install-cni-plugin_fully_configured_equal_dsts.golden"},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			defer teardown(defaultControlPlaneNamespace, t)
			controlPlaneNamespace = tc.namespace

			var buf bytes.Buffer
			err := renderCNIPlugin(&buf, tc.installCNIPluginConfig)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			actualContent := buf.String()

			expectedContent := readTestdata(t, tc.goldenFileName)
			if actualContent != expectedContent {
				writeTestdataIfUpdate(t, tc.goldenFileName, buf.Bytes())
				diffCompare(t, actualContent, expectedContent)
			}
		})
	}

	controlPlaneNamespace = defaultControlPlaneNamespace
}

func teardown(originalNamespace string, t *testing.T) {
	controlPlaneNamespace = originalNamespace
}
