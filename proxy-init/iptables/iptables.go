package iptables

import (
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	RedirectAllMode             = "redirect-all"
	RedirectListedMode          = "redirect-listed"
	IptablesPreroutingChainName = "PREROUTING"
	IptablesOutputChainName     = "OUTPUT"
)

var (
	ExecutionTraceId = strconv.Itoa(int(time.Now().Unix()))
)

type FirewallConfiguration struct {
	Mode                   string
	PortsToRedirectInbound []int
	PortsToIgnore          []int
	ProxyInboundPort       int
	ProxyOutgoingPort      int
	ProxyUid               int
	SimulateOnly           bool
}

//ConfigureFirewall configures a pod's internal iptables to redirect all desired traffic through the proxy, allowing for
// the pod to join the service mesh. A lot of this logic was based on
// https://github.com/istio/istio/blob/e83411e/pilot/docker/prepare_proxy.sh
func ConfigureFirewall(firewallConfiguration FirewallConfiguration) error {

	log.Printf("Tracing this script execution as [%s]\n", ExecutionTraceId)

	log.Println("State of iptables rules before run:")
	err := executeCommand(firewallConfiguration, makeShowAllRules())
	if err != nil {
		log.Println("Aborting firewall configuration")
		return err
	}

	commands := make([]*exec.Cmd, 0)

	commands = addIncomingTrafficRules(commands, firewallConfiguration)

	commands = addOutgoingTrafficRules(commands, firewallConfiguration)

	commands = append(commands, makeShowAllRules())

	log.Println("Executing commands:")

	for _, cmd := range commands {
		err := executeCommand(firewallConfiguration, cmd)
		if err != nil {
			log.Println("Aborting firewall configuration")
			return err
		}
	}
	return nil
}

//formatComment is used to format iptables comments in such way that it is possible to identify when the rules were added.
// This helps debug when iptables has some stale rules from previous runs, something that can happen frequently on minikube.
func formatComment(text string) string {
	return fmt.Sprintf("conduit/%s/%s", text, ExecutionTraceId)
}

func addOutgoingTrafficRules(commands []*exec.Cmd, firewallConfiguration FirewallConfiguration) []*exec.Cmd {
	ConduitOutputChainName := "CONDUIT_OUTPUT"
	executeCommand(firewallConfiguration, makeFlushChain(ConduitOutputChainName))
	executeCommand(firewallConfiguration, makeDeleteChain(ConduitOutputChainName))

	commands = append(commands, makeCreateNewChain(ConduitOutputChainName, "redirect-common-chain"))

	if firewallConfiguration.ProxyUid > 0 {
		log.Printf("Ignoring uid %d", firewallConfiguration.ProxyUid)
		commands = append(commands, makeIgnoreUserId(ConduitOutputChainName, firewallConfiguration.ProxyUid, "ignore-proxy-user-id"))
	} else {
		log.Println("Not ignoring any uid")
	}

	// Ignore loopback
	commands = append(commands, makeIgnoreLoopback(ConduitOutputChainName, "ignore-loopback"))

	log.Printf("Redirecting all OUTPUT to %d", firewallConfiguration.ProxyOutgoingPort)
	commands = append(commands, makeRedirectChainToPort(ConduitOutputChainName, firewallConfiguration.ProxyOutgoingPort, "redirect-all-outgoing-to-proxy-port"))

	//Redirect all remaining outbound traffic to the proxy.
	commands = append(commands, makeJumpFromChainToAnotherForAllProtocols(IptablesOutputChainName, ConduitOutputChainName, "install-conduit-output"))
	return commands
}

func addIncomingTrafficRules(commands []*exec.Cmd, firewallConfiguration FirewallConfiguration) []*exec.Cmd {
	ConduitRedirectChainName := "CONDUIT_REDIRECT"
	executeCommand(firewallConfiguration, makeFlushChain(ConduitRedirectChainName))
	executeCommand(firewallConfiguration, makeDeleteChain(ConduitRedirectChainName))

	commands = append(commands, makeCreateNewChain(ConduitRedirectChainName, "redirect-common-chain"))
	commands = addRulesForIgnoredPorts(firewallConfiguration, ConduitRedirectChainName, commands)
	commands = addRulesForInboundPortRedirect(firewallConfiguration, ConduitRedirectChainName, commands)

	//Redirect all remaining inbound traffic to the proxy.
	commands = append(commands, makeJumpFromChainToAnotherForAllProtocols(IptablesPreroutingChainName, ConduitRedirectChainName, "install-conduit-prerouting"))

	return commands
}

func addRulesForInboundPortRedirect(firewallConfiguration FirewallConfiguration, chainName string, commands []*exec.Cmd) []*exec.Cmd {
	if firewallConfiguration.Mode == RedirectAllMode {
		log.Print("Will redirect all INPUT ports to proxy")
		//Create a new chain for redirecting inbound and outbound traffic to the proxy port.
		commands = append(commands, makeRedirectChainToPort(chainName,
			firewallConfiguration.ProxyInboundPort,
			"redirect-all-incoming-to-proxy-port"))

	} else if firewallConfiguration.Mode == RedirectListedMode {
		log.Printf("Will redirect some INPUT ports to proxy: %v", firewallConfiguration.PortsToRedirectInbound)
		for _, port := range firewallConfiguration.PortsToRedirectInbound {
			commands = append(commands, makeRedirectChainToPortBasedOnDestinationPort(chainName,
				port,
				firewallConfiguration.ProxyInboundPort,
				fmt.Sprintf("redirect-port-%d-to-proxy-port", port)))
		}
	}
	return commands
}

func addRulesForIgnoredPorts(firewallConfiguration FirewallConfiguration, chainName string, commands []*exec.Cmd) []*exec.Cmd {
	for _, ignoredPort := range firewallConfiguration.PortsToIgnore {
		log.Printf("Will ignore port %d", ignoredPort)

		commands = append(commands, makeIgnorePort(chainName, ignoredPort, fmt.Sprintf("ignore-port-%d", ignoredPort)))
	}
	return commands
}

func executeCommand(firewallConfiguration FirewallConfiguration, cmd *exec.Cmd) error {

	log.Printf("> %s", strings.Trim(fmt.Sprintf("%v", cmd.Args), "[]"))

	if !firewallConfiguration.SimulateOnly {
		out, err := cmd.CombinedOutput()
		log.Printf("< %s\n", string(out))
		if err != nil {
			return err
		}
	}
	return nil
}

func makeIgnoreUserId(chainName string, uid int, comment string) *exec.Cmd {
	return exec.Command("iptables",
		"-t", "nat",
		"-A", chainName,
		"-m", "owner",
		"--uid-owner", strconv.Itoa(uid),
		"-j", "RETURN",
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeCreateNewChain(name string, comment string) *exec.Cmd {
	return exec.Command("iptables",
		"-t", "nat",
		"-N", name,
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeFlushChain(name string) *exec.Cmd {
	return exec.Command("iptables",
		"-t", "nat",
		"-F", name)
}

func makeDeleteChain(name string) *exec.Cmd {
	return exec.Command("iptables",
		"-t", "nat",
		"-X", name)
}

func makeRedirectChainToPort(chainName string, portToRedirect int, comment string) *exec.Cmd {
	return exec.Command("iptables",
		"-t", "nat",
		"-A", chainName,
		"-p", "tcp",
		"-j", "REDIRECT",
		"--to-port", strconv.Itoa(portToRedirect),
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeIgnorePort(chainName string, portToIgnore int, comment string) *exec.Cmd {
	return exec.Command("iptables",
		"-t", "nat",
		"-A", chainName,
		"-p", "tcp",
		"--destination-port", strconv.Itoa(portToIgnore),
		"-j", "RETURN",
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeIgnoreLoopback(chainName string, comment string) *exec.Cmd {
	return exec.Command("iptables",
		"-t", "nat",
		"-A", chainName,
		"-o", "lo",
		"-j", "RETURN",
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeRedirectChainToPortBasedOnDestinationPort(chainName string, destinationPort int, portToRedirect int, comment string) *exec.Cmd {
	return exec.Command("iptables",
		"-t", "nat",
		"-A", chainName,
		"-p", "tcp",
		"--destination-port", strconv.Itoa(destinationPort),
		"-j", "REDIRECT",
		"--to-port", strconv.Itoa(portToRedirect),
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeJumpFromChainToAnotherForAllProtocols(chainName string, targetChain string, comment string) *exec.Cmd {
	return exec.Command("iptables",
		"-t", "nat",
		"-A", chainName,
		"-j", targetChain,
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeShowAllRules() *exec.Cmd {
	return exec.Command("iptables", "-t", "nat", "-vnL")
}
