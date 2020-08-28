package proxyinjector

import (
	"encoding/base64"
	"flag"
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
	controllerNamespace := cmd.String("controller-namespace", "", "")
	cniEnabled := cmd.Bool("cni-enabled", false, "")
	clusterDomain := cmd.String("cluster-domain", "", "")
	identityScheme := cmd.String("identity-scheme", "", "scheme of the identity")
	trustDomain := cmd.String("identity-trust-domain", "", "trust domain of identity")
	encodedIdentityTrustAnchorPEM := cmd.String("identity-trust-anchors-pem", "", "Base64 encoded trust anchor certificate")
	identityIssuanceLifeTime := cmd.String("identity-issuance-lifetime", "", "")
	identityClockSkewAllowance := cmd.String("identity-clock-skew-allowance", "", "")
	omitWebHookSideEffects := cmd.Bool("omit-webhook-side-effects", false, "")
	proxyImageName := cmd.String("proxy-image-name", "", "address to serve on")
	proxyImagePullPolicy := cmd.String("proxy-image-pull-policy", "", "address to serve on")
	proxyInitImageName := cmd.String("proxy-init-image-name", "", "address to serve on")
	proxyInitImagePullPolicy := cmd.String("proxy-init-image-pull-policy", "", "address to serve on")
	controlPort := cmd.Uint64("control-port", 0, "")
	ignoreInboundPorts := cmd.String("ignore-inbound-ports", "", "")
	ignoreOutboundPorts := cmd.String("ignore-outbound-ports", "", "")
	inboundPort := cmd.Uint64("inbound-port", 0, "")
	adminPort := cmd.Uint64("admin-port", 0, "")
	outboundPort := cmd.Uint64("outbound-port", 0, "")
	proxyUID := cmd.Int64("proxy-uid", 0, "")
	logLevel := cmd.String("log-level", "", "")
	disableExternalProfiles := cmd.Bool("disable-external-profiles", false, "")
	proxyVersion := cmd.String("proxy-version", "", "")
	proxyInitImageVersion := cmd.String("proxy-init-image-version", "", "")
	debugImageName := cmd.String("debug-image-name", "", "")
	debugImagePullPolicy := cmd.String("debug-image", "", "")
	debugImageVersion := cmd.String("debug-image-version", "", "")
	destinationGetNetworks := cmd.String("destination-get-networks", "", "")
	logFormat := cmd.String("log-format", "", "")
	outBoundConnectTimeout := cmd.String("outbound-connect-timeout", "", "")
	inboundConnectTimeOut := cmd.String("inbound-connect-timeout", "", "")
	proxyCPURequest := cmd.String("proxy-cpu-request", "", "")
	proxyCPULimit := cmd.String("proxy-cpu-limit", "", "")
	proxyMemoryRequest := cmd.String("proxy-memory-request", "", "")
	proxyMemoryLimit := cmd.String("proxy-memory-limit", "", "")

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

	identityTrustAnchorPEM := string(rawPEM)

	global := config.Global{
		LinkerdNamespace: *controllerNamespace,
		CniEnabled:       *cniEnabled,
		ClusterDomain:    *clusterDomain,
		IdentityContext: &config.IdentityContext{
			TrustDomain:        *trustDomain,
			TrustAnchorsPem:    identityTrustAnchorPEM,
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
		LogLevel:                &config.LogLevel{Level: *logLevel},
		DisableExternalProfiles: *disableExternalProfiles,
		ProxyVersion:            *proxyVersion,
		ProxyInitImageVersion:   *proxyInitImageVersion,
		DebugImage: &config.Image{
			ImageName:  *debugImageName,
			PullPolicy: *debugImagePullPolicy,
		},
		DebugImageVersion:      *debugImageVersion,
		DestinationGetNetworks: *destinationGetNetworks,
		LogFormat:              *logFormat,
		OutboundConnectTimeout: *outBoundConnectTimeout,
		InboundConnectTimeout:  *inboundConnectTimeOut,
	}

	injection := injector.NewInjection(&global, &proxy)
	webhook.Launch(
		[]k8s.APIResource{k8s.NS, k8s.Deploy, k8s.RC, k8s.RS, k8s.Job, k8s.DS, k8s.SS, k8s.Pod, k8s.CJ},
		9995,
		injection.Inject,
		"linkerd-proxy-injector",
		"proxy-injector",
		args,
	)
}

func toPortRanges(portRanges []string) []*config.PortRange {
	ports := make([]*config.PortRange, len(portRanges))
	for i, p := range portRanges {
		ports[i] = &config.PortRange{PortRange: p}
	}
	return ports
}
