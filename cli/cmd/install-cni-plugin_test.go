package cmd

import (
	"bytes"
	"fmt"
	"testing"
)

func TestRenderCNIPlugin(t *testing.T) {
	defaultControlPlaneNamespace := controlPlaneNamespace
	defaultOptions, err := newCNIInstallOptionsWithDefaults()
	if err != nil {
		t.Fatalf("Unexpected error from newCNIInstallOptionsWithDefaults(): %v", err)
	}

	fullyConfiguredOptions := &cniPluginOptions{
		linkerdVersion:      "awesome-linkerd-version.1",
		dockerRegistry:      "gcr.io/linkerd-io",
		proxyControlPort:    5190,
		proxyAdminPort:      5191,
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

	otherNamespace := "other"

	fullyConfiguredOptionsEqualDsts := &cniPluginOptions{
		linkerdVersion:      "awesome-linkerd-version.1",
		dockerRegistry:      "gcr.io/linkerd-io",
		proxyControlPort:    5190,
		proxyAdminPort:      5191,
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

	testCases := []struct {
		*cniPluginOptions
		namespace      string
		goldenFileName string
	}{
		{defaultOptions, defaultControlPlaneNamespace, "install-cni-plugin_default.golden"},
		{fullyConfiguredOptions, otherNamespace, "install-cni-plugin_fully_configured.golden"},
		{fullyConfiguredOptionsEqualDsts, otherNamespace, "install-cni-plugin_fully_configured_equal_dsts.golden"},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			defer teardown(defaultControlPlaneNamespace)
			controlPlaneNamespace = tc.namespace

			var buf bytes.Buffer
			err := renderCNIPlugin(&buf, tc.cniPluginOptions)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			diffTestdata(t, tc.goldenFileName, buf.String())
		})
	}

	controlPlaneNamespace = defaultControlPlaneNamespace
}

func teardown(originalNamespace string) {
	controlPlaneNamespace = originalNamespace
}
