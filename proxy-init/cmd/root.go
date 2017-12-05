package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/runconduit/conduit/proxy-init/iptables"
	"github.com/spf13/cobra"
)

var incomingProxyPort int
var outgoingProxyPort int
var proxyUserId int
var portsToRedirect []int
var portsToIgnore []int
var simulateOnly bool

var RootCmd = &cobra.Command{
	Use:   "proxy-init",
	Short: "Adds a Kubernetes pod to join the Conduit Service Mesh",
	Long: `proxy-init Adds a Kubernetes pod to join the Conduit Service Mesh.

Find more information at https://conduit.io/.`,
	Run: func(cmd *cobra.Command, args []string) {
		err := iptables.ConfigureFirewall(buildFirewallConfiguration())
		if err != nil {
			log.Fatal(err)
		}
	},
}

func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
}

func init() {
	RootCmd.PersistentFlags().IntVarP(&incomingProxyPort, "incoming-proxy-port", "p", -1, "Port to redirect incoming traffic")
	RootCmd.PersistentFlags().IntVarP(&outgoingProxyPort, "outgoing-proxy-port", "o", -1, "Port to redirect outgoing traffic")
	RootCmd.PersistentFlags().BoolVar(&simulateOnly, "simulate", false, "Don't execute any command, just print what would be executed")
	RootCmd.PersistentFlags().IntSliceVarP(&portsToRedirect, "ports-to-redirect", "r", make([]int, 0), "Port to redirect to proxy, if no port is specified then ALL ports are redirected")
	RootCmd.PersistentFlags().IntSliceVarP(&portsToIgnore, "ports-to-ignore", "i", make([]int, 0), "Port to ignore and not redirect to proxy. This has higher precedence than any other parameters.")
	RootCmd.PersistentFlags().IntVarP(&proxyUserId, "proxy-uid", "u", -1, "User ID that the proxy is running under. Any traffic coming from this user will be ignored to avoid infinite redirection loops.")
}

func buildFirewallConfiguration() iptables.FirewallConfiguration {
	if incomingProxyPort < 0 || incomingProxyPort > 65535 {
		fmt.Println("--incoming-proxy-port must be a valid TCP port number")
		os.Exit(1)
	}

	if outgoingProxyPort < 0 || incomingProxyPort > 65535 {
		fmt.Println("--outgoing-proxy-port must be a valid TCP port number")
		os.Exit(1)
	}

	firewallConfiguration := iptables.FirewallConfiguration{}

	if len(portsToRedirect) > 0 {
		firewallConfiguration.Mode = iptables.RedirectListedMode
	} else {
		firewallConfiguration.Mode = iptables.RedirectAllMode
	}

	firewallConfiguration.PortsToRedirectInbound = portsToRedirect
	firewallConfiguration.PortsToIgnore = portsToIgnore
	firewallConfiguration.ProxyInboundPort = incomingProxyPort
	firewallConfiguration.ProxyOutgoingPort = outgoingProxyPort
	firewallConfiguration.ProxyUid = proxyUserId
	firewallConfiguration.SimulateOnly = simulateOnly
	return firewallConfiguration
}
