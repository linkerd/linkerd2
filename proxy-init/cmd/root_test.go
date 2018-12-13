package cmd

import (
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/proxy-init/iptables"
)

func TestBuildFirewallConfiguration(t *testing.T) {
	t.Run("It produces a FirewallConfiguration for the default config", func(t *testing.T) {
		expectedIncomingProxyPort := 1234
		expectedOutgoingProxyPort := 2345
		expectedProxyUserID := 33
		expectedConfig := &iptables.FirewallConfiguration{
			Mode:                   iptables.RedirectAllMode,
			PortsToRedirectInbound: make([]int, 0),
			InboundPortsToIgnore:   make([]int, 0),
			OutboundPortsToIgnore:  make([]int, 0),
			ProxyInboundPort:       expectedIncomingProxyPort,
			ProxyOutgoingPort:      expectedOutgoingProxyPort,
			ProxyUID:               expectedProxyUserID,
			SimulateOnly:           false,
		}

		options := newRootOptions()
		options.incomingProxyPort = expectedIncomingProxyPort
		options.outgoingProxyPort = expectedOutgoingProxyPort
		options.proxyUserID = expectedProxyUserID

		config, err := buildFirewallConfiguration(options)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		if !reflect.DeepEqual(config, expectedConfig) {
			t.Fatalf("Expected config [%v] but got [%v]", expectedConfig, config)
		}
	})

	t.Run("It rejects invalid config options", func(t *testing.T) {
		for _, tt := range []struct {
			options      *rootOptions
			errorMessage string
		}{
			{
				options: &rootOptions{
					incomingProxyPort: -1,
					outgoingProxyPort: 1234,
				},
				errorMessage: "--incoming-proxy-port must be a valid TCP port number",
			},
			{
				options: &rootOptions{
					incomingProxyPort: 100000,
					outgoingProxyPort: 1234,
				},
				errorMessage: "--incoming-proxy-port must be a valid TCP port number",
			},
			{
				options: &rootOptions{
					incomingProxyPort: 1234,
					outgoingProxyPort: -1,
				},
				errorMessage: "--outgoing-proxy-port must be a valid TCP port number",
			},
			{
				options: &rootOptions{
					incomingProxyPort: 1234,
					outgoingProxyPort: 100000,
				},
				errorMessage: "--outgoing-proxy-port must be a valid TCP port number",
			},
		} {
			_, err := buildFirewallConfiguration(tt.options)
			if err == nil {
				t.Fatalf("Expected error for config [%v], got nil", tt.options)
			}
			if err.Error() != tt.errorMessage {
				t.Fatalf("Expected error [%s] for config [%v], got [%s]",
					tt.errorMessage, tt.options, err.Error())
			}
		}
	})
}
