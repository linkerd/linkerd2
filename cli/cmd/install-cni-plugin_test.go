package cmd

import (
	"bytes"
	"fmt"
	"testing"

	"helm.sh/helm/v3/pkg/cli/values"
)

func TestRenderCNIPlugin(t *testing.T) {
	defaultOptions, err := newCNIInstallOptionsWithDefaults()
	if err != nil {
		t.Fatalf("Unexpected error from newCNIInstallOptionsWithDefaults(): %v", err)
	}

	image := cniPluginImage{
		name:       "my-docker-registry.io/awesome/cni-plugin-test-image",
		version:    "v1.3.0",
		pullPolicy: nil,
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
		image:               image,
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
		image:               image,
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
		image:               image,
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

	valOpts := values.Options{
		Values: []string{"resources.cpu.limit=1m"},
	}

	testCases := []struct {
		*cniPluginOptions
		valOpts        values.Options
		goldenFileName string
	}{
		{defaultOptions, valOpts, "install-cni-plugin_default.golden"},
		{fullyConfiguredOptions, values.Options{}, "install-cni-plugin_fully_configured.golden"},
		{fullyConfiguredOptionsEqualDsts, values.Options{}, "install-cni-plugin_fully_configured_equal_dsts.golden"},
		{fullyConfiguredOptionsNoNamespace, values.Options{}, "install-cni-plugin_fully_configured_no_namespace.golden"},
		{defaultOptionsWithSkipPorts, values.Options{}, "install-cni-plugin_skip_ports.golden"},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			var buf bytes.Buffer
			err := renderCNIPlugin(&buf, tc.valOpts, tc.cniPluginOptions)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if err = testDataDiffer.DiffTestYAML(tc.goldenFileName, buf.String()); err != nil {
				t.Error(err)
			}
		})
	}
}
