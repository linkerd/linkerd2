package proxyinjector

import (
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/controller/k8s"
	injector "github.com/linkerd/linkerd2/controller/proxy-injector"
	"github.com/linkerd/linkerd2/controller/webhook"
	"github.com/linkerd/linkerd2/pkg/flags"
)

// Main executes the proxy-injector subcommand
func Main(args []string) {

	cmd := flag.NewFlagSet("proxy-injector", flag.ExitOnError)
	controllerNamespace := cmd.String("controller-namespace", "", "namespace in which Linkerd is installed")
	cniEnabled := cmd.Bool("cni-enabled", false, "enabling this omits the the proxy-init container when injecting the proxy; requires the linkerd-cni plugin to already be installed")
	clusterDomain := cmd.String("cluster-domain", "", "kubernetes cluster domain")
	identityScheme := cmd.String("identity-scheme", "", "scheme used for the identity issuer secret format")
	trustDomain := cmd.String("identity-trust-domain", "", "configures the name suffix used for identities")
	encodedIdentityTrustAnchorPEM := cmd.String("identity-trust-anchors-pem", "", "base64 encoded trust anchors certificate")
	identityIssuanceLifeTime := cmd.String("identity-issuance-lifetime", "", "the amount of time for which the Identity issuer should certify identity")
	identityClockSkewAllowance := cmd.String("identity-clock-skew-allowance", "", "the amount of time to allow for clock skew within a Linkerd cluster")
	omitWebHookSideEffects := cmd.Bool("omit-webhook-side-effects", false, "omit the sideEffects flag in the webhook manifests")
	proxyImageName := cmd.String("proxy-image", "", "linkerd proxy container image name")
	proxyImagePullPolicy := cmd.String("proxy-image-pull-policy", "", "linkerd proxy image's pull policy")
	proxyInitImageName := cmd.String("proxy-init-image", "", "linkerd proxy init container image name")
	proxyInitImagePullPolicy := cmd.String("proxy-init-image-pull-policy", "", "linkerd proxy init image's pull policy")
	controlPort := cmd.Uint64("control-port", 0, "proxy port to use for control")
	ignoreInboundPorts := cmd.String("ignore-inbound-ports", "", "ports and/or port ranges (inclusive) that should skip the proxy and send directly to the application")
	ignoreOutboundPorts := cmd.String("ignore-outbound-ports", "", "outbound ports and/or port ranges (inclusive) that should skip the proxy")
	inboundPort := cmd.Uint64("inbound-port", 0, "proxy port to use for inbound traffic")
	adminPort := cmd.Uint64("admin-port", 0, "proxy port to serve metrics on")
	outboundPort := cmd.Uint64("outbound-port", 0, "proxy port to use for outbound traffic")
	proxyUID := cmd.Int64("proxy-uid", 0, "run the proxy under this user ID")
	proxyLogLevel := cmd.String("proxy-log-level", "", "log level for the proxy")
	disableExternalProfiles := cmd.Bool("disable-external-profiles", false, "disable service profiles for non-Kubernetes services")
	proxyVersion := cmd.String("proxy-version", "", "tag to be used for the Linkerd proxy images")
	proxyInitImageVersion := cmd.String("proxy-init-image-version", "", "linkerd init container image version")
	debugImageName := cmd.String("debug-image", "", "linkerd debug container image name")
	debugImagePullPolicy := cmd.String("debug-image-pull-policy", "", "docker image pull policy for debug image")
	debugImageVersion := cmd.String("debug-image-version", "", "linkerd debug container image version")
	destinationGetNetworks := cmd.String("destination-get-networks", "", "network ranges for which the Linkerd proxy does destination lookups by IP address")
	proxyLogFormat := cmd.String("proxy-log-format", "", "log format (`plain` or `json`) for the proxy")
	outboundConnectTimeout := cmd.String("outbound-connect-timeout", "", "maximum time allowed for the proxy to establish an outbound TCP connection")
	inboundConnectTimeOut := cmd.String("inbound-connect-timeout", "", "maximum time allowed for the proxy to establish an inbound TCP connection")
	proxyCPURequest := cmd.String("proxy-cpu-request", "", "amount of CPU units that the proxy sidecar requests")
	proxyCPULimit := cmd.String("proxy-cpu-limit", "", "maximum amount of CPU units that the proxy sidecar can use")
	proxyMemoryRequest := cmd.String("proxy-memory-request", "", "amount of Memory that the proxy sidecar requests")
	proxyMemoryLimit := cmd.String("proxy-memory-limit", "", "maximum amount of Memory that the proxy sidecar can use")
	metricsAddr := cmd.String("metrics-addr", fmt.Sprintf(":%d", 9995), "address to serve scrapable metrics on")
	addr := cmd.String("addr", ":8443", "address to serve on")
	kubeconfig := cmd.String("kubeconfig", "", "path to kubeconfig")

	flags.ConfigureAndParse(cmd, args)

	identityIssuanceLifeTimeDuration, err := time.ParseDuration(*identityIssuanceLifeTime)
	if err != nil {
		log.Fatalf("cannot convert identity issuance lifetime from string to duration: %s", err)
	}

	identityClockSkewAllowanceDuration, err := time.ParseDuration(*identityClockSkewAllowance)
	if err != nil {
		log.Fatalf("cannot convert clock skew allowance from string to duration: %s", err)
	}

	rawPEM, err := base64.StdEncoding.DecodeString(*encodedIdentityTrustAnchorPEM)
	if err != nil {
		log.Fatalf("could not decode identity trust anchors PEM: %s", err.Error())
	}

	global := config.Global{
		LinkerdNamespace: *controllerNamespace,
		CniEnabled:       *cniEnabled,
		ClusterDomain:    *clusterDomain,
		IdentityContext: &config.IdentityContext{
			TrustDomain:        *trustDomain,
			TrustAnchorsPem:    string(rawPEM),
			Scheme:             *identityScheme,
			IssuanceLifetime:   ptypes.DurationProto(identityIssuanceLifeTimeDuration),
			ClockSkewAllowance: ptypes.DurationProto(identityClockSkewAllowanceDuration),
		},
		OmitWebhookSideEffects: *omitWebHookSideEffects,
	}

	proxy := config.Proxy{
		ProxyImage: &config.Image{
			ImageName:  *proxyImageName,
			PullPolicy: *proxyImagePullPolicy,
		},
		ProxyInitImage: &config.Image{
			ImageName:  *proxyInitImageName,
			PullPolicy: *proxyInitImagePullPolicy,
		},
		ControlPort:         &config.Port{Port: uint32(*controlPort)},
		IgnoreInboundPorts:  toPortRanges(strings.Split(*ignoreInboundPorts, ",")),
		IgnoreOutboundPorts: toPortRanges(strings.Split(*ignoreOutboundPorts, ",")),
		InboundPort:         &config.Port{Port: uint32(*inboundPort)},
		AdminPort:           &config.Port{Port: uint32(*adminPort)},
		OutboundPort:        &config.Port{Port: uint32(*outboundPort)},
		Resource: &config.ResourceRequirements{
			RequestCpu:    *proxyCPURequest,
			RequestMemory: *proxyMemoryRequest,
			LimitCpu:      *proxyCPULimit,
			LimitMemory:   *proxyMemoryLimit,
		},
		ProxyUid:                *proxyUID,
		LogLevel:                &config.LogLevel{Level: *proxyLogLevel},
		DisableExternalProfiles: *disableExternalProfiles,
		ProxyVersion:            *proxyVersion,
		ProxyInitImageVersion:   *proxyInitImageVersion,
		DebugImage: &config.Image{
			ImageName:  *debugImageName,
			PullPolicy: *debugImagePullPolicy,
		},
		DebugImageVersion:      *debugImageVersion,
		DestinationGetNetworks: *destinationGetNetworks,
		LogFormat:              *proxyLogFormat,
		OutboundConnectTimeout: *outboundConnectTimeout,
		InboundConnectTimeout:  *inboundConnectTimeOut,
	}

	injection := injector.NewInjection(&global, &proxy)
	webhook.Launch(
		[]k8s.APIResource{k8s.NS, k8s.Deploy, k8s.RC, k8s.RS, k8s.Job, k8s.DS, k8s.SS, k8s.Pod, k8s.CJ},
		injection.Inject,
		"linkerd-proxy-injector",
		*kubeconfig,
		*addr,
		*metricsAddr,
	)
}

func toPortRanges(portRanges []string) []*config.PortRange {
	ports := make([]*config.PortRange, len(portRanges))
	for i, p := range portRanges {
		ports[i] = &config.PortRange{PortRange: p}
	}
	return ports
}
