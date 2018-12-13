package cmd

import (
	"fmt"

	"github.com/linkerd/linkerd2/proxy-init/iptables"
	"github.com/spf13/cobra"
)

type rootOptions struct {
	incomingProxyPort     int
	outgoingProxyPort     int
	proxyUserID           int
	portsToRedirect       []int
	inboundPortsToIgnore  []int
	outboundPortsToIgnore []int
	simulateOnly          bool
}

func newRootOptions() *rootOptions {
	return &rootOptions{
		incomingProxyPort:     -1,
		outgoingProxyPort:     -1,
		proxyUserID:           -1,
		portsToRedirect:       make([]int, 0),
		inboundPortsToIgnore:  make([]int, 0),
		outboundPortsToIgnore: make([]int, 0),
		simulateOnly:          false,
	}
}

func NewRootCmd() *cobra.Command {
	options := newRootOptions()

	cmd := &cobra.Command{
		Use:   "proxy-init",
		Short: "proxy-init adds a Kubernetes pod to the Linkerd service mesh",
		Long:  "proxy-init adds a Kubernetes pod to the Linkerd service mesh.",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := buildFirewallConfiguration(options)
			if err != nil {
				return err
			}
			return iptables.ConfigureFirewall(*config)
		},
	}

	cmd.PersistentFlags().IntVarP(&options.incomingProxyPort, "incoming-proxy-port", "p", options.incomingProxyPort, "Port to redirect incoming traffic")
	cmd.PersistentFlags().IntVarP(&options.outgoingProxyPort, "outgoing-proxy-port", "o", options.outgoingProxyPort, "Port to redirect outgoing traffic")
	cmd.PersistentFlags().IntVarP(&options.proxyUserID, "proxy-uid", "u", options.proxyUserID, "User ID that the proxy is running under. Any traffic coming from this user will be ignored to avoid infinite redirection loops.")
	cmd.PersistentFlags().IntSliceVarP(&options.portsToRedirect, "ports-to-redirect", "r", options.portsToRedirect, "Port to redirect to proxy, if no port is specified then ALL ports are redirected")
	cmd.PersistentFlags().IntSliceVar(&options.inboundPortsToIgnore, "inbound-ports-to-ignore", options.inboundPortsToIgnore, "Inbound ports to ignore and not redirect to proxy. This has higher precedence than any other parameters.")
	cmd.PersistentFlags().IntSliceVar(&options.outboundPortsToIgnore, "outbound-ports-to-ignore", options.outboundPortsToIgnore, "Outbound ports to ignore and not redirect to proxy. This has higher precedence than any other parameters.")
	cmd.PersistentFlags().BoolVar(&options.simulateOnly, "simulate", options.simulateOnly, "Don't execute any command, just print what would be executed")

	return cmd
}

func buildFirewallConfiguration(options *rootOptions) (*iptables.FirewallConfiguration, error) {
	if options.incomingProxyPort < 0 || options.incomingProxyPort > 65535 {
		return nil, fmt.Errorf("--incoming-proxy-port must be a valid TCP port number")
	}

	if options.outgoingProxyPort < 0 || options.outgoingProxyPort > 65535 {
		return nil, fmt.Errorf("--outgoing-proxy-port must be a valid TCP port number")
	}

	firewallConfiguration := &iptables.FirewallConfiguration{
		ProxyInboundPort:       options.incomingProxyPort,
		ProxyOutgoingPort:      options.outgoingProxyPort,
		ProxyUID:               options.proxyUserID,
		PortsToRedirectInbound: options.portsToRedirect,
		InboundPortsToIgnore:   options.inboundPortsToIgnore,
		OutboundPortsToIgnore:  options.outboundPortsToIgnore,
		SimulateOnly:           options.simulateOnly,
	}

	if len(options.portsToRedirect) > 0 {
		firewallConfiguration.Mode = iptables.RedirectListedMode
	} else {
		firewallConfiguration.Mode = iptables.RedirectAllMode
	}

	return firewallConfiguration, nil
}
