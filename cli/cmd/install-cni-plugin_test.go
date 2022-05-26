package cmd

import (
	"bytes"
	"fmt"
	"testing"
)

func TestRenderCNIPlugin(t *testing.T) {
	defaultOptions, err := newCNIInstallOptionsWithDefaults()
	if err != nil {
		t.Fatalf("Unexpected error from newCNIInstallOptionsWithDefaults(): %v", err)
	}

	fullyConfiguredOptions := &cniPluginOptions{
		linkerdVersion:      "awesome-linkerd-version.1",
		dockerRegistry:      "cr.l5d.io/linkerd",
		proxyControlPort:    5190,
		proxyAdminPort:      5191,
		inboundPort:         5143,
		outboundPort:        5140,
		ignoreInboundPorts:  make([]string, 0),
		ignoreOutboundPorts: make([]string, 0),
		proxyUID:            12102,
		cniPluginImage:      "my-docker-registry.io/awesome/cni-plugin-test-image",
		logLevel:            "debug",
		destCNINetDir:       "/etc/kubernetes/cni/net.d",
		destCNIBinDir:       "/opt/my-cni/bin",
		priorityClassName:   "system-node-critical",
	}

	fullyConfiguredOptionsEqualDsts := &cniPluginOptions{
		linkerdVersion:      "awesome-linkerd-version.1",
		dockerRegistry:      "cr.l5d.io/linkerd",
		proxyControlPort:    5190,
		proxyAdminPort:      5191,
		inboundPort:         5143,
		outboundPort:        5140,
		ignoreInboundPorts:  make([]string, 0),
		ignoreOutboundPorts: make([]string, 0),
		proxyUID:            12102,
		cniPluginImage:      "my-docker-registry.io/awesome/cni-plugin-test-image",
		logLevel:            "debug",
		destCNINetDir:       "/etc/kubernetes/cni/net.d",
		destCNIBinDir:       "/etc/kubernetes/cni/net.d",
		priorityClassName:   "system-node-critical",
	}

	fullyConfiguredOptionsNoNamespace := &cniPluginOptions{
		linkerdVersion:      "awesome-linkerd-version.1",
		dockerRegistry:      "cr.l5d.io/linkerd",
		proxyControlPort:    5190,
		proxyAdminPort:      5191,
		inboundPort:         5143,
		outboundPort:        5140,
		ignoreInboundPorts:  make([]string, 0),
		ignoreOutboundPorts: make([]string, 0),
		proxyUID:            12102,
		cniPluginImage:      "my-docker-registry.io/awesome/cni-plugin-test-image",
		logLevel:            "debug",
		destCNINetDir:       "/etc/kubernetes/cni/net.d",
		destCNIBinDir:       "/opt/my-cni/bin",
		priorityClassName:   "system-node-critical",
	}

	defaultOptionsWithSkipPorts, err := newCNIInstallOptionsWithDefaults()
	if err != nil {
		t.Fatalf("Unexpected error from newCNIInstallOptionsWithDefaults(): %v", err)
	}

	defaultOptionsWithSkipPorts.ignoreInboundPorts = append(defaultOptionsWithSkipPorts.ignoreInboundPorts, []string{"80", "8080"}...)
	defaultOptionsWithSkipPorts.ignoreOutboundPorts = append(defaultOptionsWithSkipPorts.ignoreOutboundPorts, []string{"443", "1000"}...)

	testCases := []struct {
		*cniPluginOptions
		goldenFileName string
	}{
		{defaultOptions, "install-cni-plugin_default.golden"},
		{fullyConfiguredOptions, "install-cni-plugin_fully_configured.golden"},
		{fullyConfiguredOptionsEqualDsts, "install-cni-plugin_fully_configured_equal_dsts.golden"},
		{fullyConfiguredOptionsNoNamespace, "install-cni-plugin_fully_configured_no_namespace.golden"},
		{defaultOptionsWithSkipPorts, "install-cni-plugin_skip_ports.golden"},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			var buf bytes.Buffer
			err := renderCNIPlugin(&buf, tc.cniPluginOptions)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if err = testDataDiffer.DiffTestYAML(tc.goldenFileName, buf.String()); err != nil {
				t.Error(err)
			}
		})
	}
}
