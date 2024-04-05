package inject

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/util"
	log "github.com/sirupsen/logrus"
	k8sResource "k8s.io/apimachinery/pkg/api/resource"
)

type ConfigLookup interface {
	GetConfigOverride(string) (string, bool)
	GetInboundPorts() string
	ParseOpaqueOverrides(string) []util.PortRange
}

// AppendNamespaceAnnotations allows pods to inherit config specific annotations
// from the namespace they belong to. If the namespace has a valid config key
// that the pod does not, then it is appended to the pod's template
func AppendNamespaceAnnotations(base map[string]string, nsAnn map[string]string, conf ConfigLookup) {
	ann := append(ProxyAnnotations, ProxyAlphaConfigAnnotations...)
	ann = append(ann, k8s.ProxyInjectAnnotation)

	for _, key := range ann {
		if _, found := nsAnn[key]; !found {
			continue
		}
		if val, ok := conf.GetConfigOverride(key); ok {
			base[key] = val
		}
	}
}

// GetOverriddenValues returns the final Values struct which is created
// by overriding annotated configuration on top of default Values
func GetOverriddenValues(values *l5dcharts.Values, overrides map[string]string, conf ConfigLookup) (*l5dcharts.Values, error) {
	// Make a copy of Values and mutate that
	copyValues, err := values.DeepCopy()
	if err != nil {
		return nil, err
	}

	copyValues.Proxy.PodInboundPorts = conf.GetInboundPorts()
	applyAnnotationOverrides(copyValues, overrides, conf)
	return copyValues, nil
}

func applyAnnotationOverrides(values *l5dcharts.Values, annotations map[string]string, conf ConfigLookup) {

	if override, ok := annotations[k8s.ProxyInjectAnnotation]; ok {
		if override == k8s.ProxyInjectIngress {
			values.Proxy.IsIngress = true
		}
	}

	if override, ok := annotations[k8s.ProxyImageAnnotation]; ok {
		values.Proxy.Image.Name = override
	}

	if override, ok := annotations[k8s.ProxyVersionOverrideAnnotation]; ok {
		values.Proxy.Image.Version = override
	}

	if override, ok := annotations[k8s.ProxyImagePullPolicyAnnotation]; ok {
		values.Proxy.Image.PullPolicy = override
	}

	if override, ok := annotations[k8s.ProxyInitImageVersionAnnotation]; ok {
		values.ProxyInit.Image.Version = override
	}

	if override, ok := annotations[k8s.ProxyControlPortAnnotation]; ok {
		controlPort, err := strconv.ParseInt(override, 10, 32)
		if err == nil {
			values.Proxy.Ports.Control = int32(controlPort)
		}
	}

	if override, ok := annotations[k8s.ProxyInboundPortAnnotation]; ok {
		inboundPort, err := strconv.ParseInt(override, 10, 32)
		if err == nil {
			values.Proxy.Ports.Inbound = int32(inboundPort)
		}
	}

	if override, ok := annotations[k8s.ProxyAdminPortAnnotation]; ok {
		adminPort, err := strconv.ParseInt(override, 10, 32)
		if err == nil {
			values.Proxy.Ports.Admin = int32(adminPort)
		}
	}

	if override, ok := annotations[k8s.ProxyOutboundPortAnnotation]; ok {
		outboundPort, err := strconv.ParseInt(override, 10, 32)
		if err == nil {
			values.Proxy.Ports.Outbound = int32(outboundPort)
		}
	}

	if override, ok := annotations[k8s.ProxyPodInboundPortsAnnotation]; ok {
		values.Proxy.PodInboundPorts = override
	}

	if override, ok := annotations[k8s.ProxyLogLevelAnnotation]; ok {
		values.Proxy.LogLevel = override
	}

	if override, ok := annotations[k8s.ProxyLogFormatAnnotation]; ok {
		values.Proxy.LogFormat = override
	}

	if override, ok := annotations[k8s.ProxyRequireIdentityOnInboundPortsAnnotation]; ok {
		values.Proxy.RequireIdentityOnInboundPorts = override
	}

	if override, ok := annotations[k8s.ProxyOutboundConnectTimeout]; ok {
		duration, err := time.ParseDuration(override)
		if err != nil {
			log.Warnf("unrecognized proxy-outbound-connect-timeout duration value found on pod annotation: %s", err.Error())
		} else {
			values.Proxy.OutboundConnectTimeout = fmt.Sprintf("%dms", int(duration.Seconds()*1000))
		}
	}

	if override, ok := annotations[k8s.ProxyInboundConnectTimeout]; ok {
		duration, err := time.ParseDuration(override)
		if err != nil {
			log.Warnf("unrecognized proxy-inbound-connect-timeout duration value found on pod annotation: %s", err.Error())
		} else {
			values.Proxy.InboundConnectTimeout = fmt.Sprintf("%dms", int(duration.Seconds()*1000))
		}
	}

	if override, ok := annotations[k8s.ProxyOutboundDiscoveryCacheUnusedTimeout]; ok {
		duration, err := time.ParseDuration(override)
		if err != nil {
			log.Warnf("unrecognized duration value used on pod annotation %s: %s", k8s.ProxyOutboundDiscoveryCacheUnusedTimeout, err.Error())
		} else {
			values.Proxy.OutboundDiscoveryCacheUnusedTimeout = fmt.Sprintf("%ds", int(duration.Seconds()))
		}
	}

	if override, ok := annotations[k8s.ProxyInboundDiscoveryCacheUnusedTimeout]; ok {
		duration, err := time.ParseDuration(override)
		if err != nil {
			log.Warnf("unrecognized duration value used on pod annotation %s: %s", k8s.ProxyInboundDiscoveryCacheUnusedTimeout, err.Error())
		} else {
			values.Proxy.InboundDiscoveryCacheUnusedTimeout = fmt.Sprintf("%ds", int(duration.Seconds()))
		}
	}

	if override, ok := annotations[k8s.ProxyDisableOutboundProtocolDetectTimeout]; ok {
		value, err := strconv.ParseBool(override)
		if err == nil {
			values.Proxy.DisableOutboundProtocolDetectTimeout = value
		} else {
			log.Warnf("unrecognised value used on pod annotation %s: %s", k8s.ProxyDisableOutboundProtocolDetectTimeout, err.Error())
		}
	}

	if override, ok := annotations[k8s.ProxyDisableInboundProtocolDetectTimeout]; ok {
		value, err := strconv.ParseBool(override)
		if err == nil {
			values.Proxy.DisableInboundProtocolDetectTimeout = value
		} else {
			log.Warnf("unrecognised value used on pod annotation %s: %s", k8s.ProxyDisableInboundProtocolDetectTimeout, err.Error())
		}
	}

	if override, ok := annotations[k8s.ProxyShutdownGracePeriodAnnotation]; ok {
		duration, err := time.ParseDuration(override)
		if err != nil {
			log.Warnf("unrecognized proxy-shutdown-grace-period duration value found on pod annotation: %s", err.Error())
		} else {
			values.Proxy.ShutdownGracePeriod = fmt.Sprintf("%dms", int(duration.Seconds()*1000))
		}
	}

	if override, ok := annotations[k8s.ProxyEnableGatewayAnnotation]; ok {
		value, err := strconv.ParseBool(override)
		if err == nil {
			values.Proxy.IsGateway = value
		}
	}

	if override, ok := annotations[k8s.ProxyWaitBeforeExitSecondsAnnotation]; ok {
		waitBeforeExitSeconds, err := strconv.ParseUint(override, 10, 64)
		if nil != err {
			log.Warnf("unrecognized value used for the %s annotation, uint64 is expected: %s",
				k8s.ProxyWaitBeforeExitSecondsAnnotation, override)
		} else {
			values.Proxy.WaitBeforeExitSeconds = waitBeforeExitSeconds
		}
	}

	if override, ok := annotations[k8s.ProxyEnableNativeSidecarAnnotation]; ok {
		value, err := strconv.ParseBool(override)
		if err == nil {
			values.Proxy.NativeSidecar = value
		}
	}

	if override, ok := annotations[k8s.ProxyCPURequestAnnotation]; ok {
		_, err := k8sResource.ParseQuantity(override)
		if err != nil {
			log.Warnf("%s (%s)", err, k8s.ProxyCPURequestAnnotation)
		} else {
			values.Proxy.Resources.CPU.Request = override
		}
	}

	if override, ok := annotations[k8s.ProxyMemoryRequestAnnotation]; ok {
		_, err := k8sResource.ParseQuantity(override)
		if err != nil {
			log.Warnf("%s (%s)", err, k8s.ProxyMemoryRequestAnnotation)
		} else {
			values.Proxy.Resources.Memory.Request = override
		}
	}

	if override, ok := annotations[k8s.ProxyEphemeralStorageRequestAnnotation]; ok {
		_, err := k8sResource.ParseQuantity(override)
		if err != nil {
			log.Warnf("%s (%s)", err, k8s.ProxyEphemeralStorageRequestAnnotation)
		} else {
			values.Proxy.Resources.EphemeralStorage.Request = override
		}
	}

	if override, ok := annotations[k8s.ProxyCPULimitAnnotation]; ok {
		q, err := k8sResource.ParseQuantity(override)
		if err != nil {
			log.Warnf("%s (%s)", err, k8s.ProxyCPULimitAnnotation)
		} else {
			values.Proxy.Resources.CPU.Limit = override

			n, err := ToWholeCPUCores(q)
			if err != nil {
				log.Warnf("%s (%s)", err, k8s.ProxyCPULimitAnnotation)
			}
			values.Proxy.Cores = n
		}
	}

	if override, ok := annotations[k8s.ProxyMemoryLimitAnnotation]; ok {
		_, err := k8sResource.ParseQuantity(override)
		if err != nil {
			log.Warnf("%s (%s)", err, k8s.ProxyMemoryLimitAnnotation)
		} else {
			values.Proxy.Resources.Memory.Limit = override
		}
	}

	if override, ok := annotations[k8s.ProxyEphemeralStorageLimitAnnotation]; ok {
		_, err := k8sResource.ParseQuantity(override)
		if err != nil {
			log.Warnf("%s (%s)", err, k8s.ProxyEphemeralStorageLimitAnnotation)
		} else {
			values.Proxy.Resources.EphemeralStorage.Limit = override
		}
	}

	if override, ok := annotations[k8s.ProxyUIDAnnotation]; ok {
		v, err := strconv.ParseInt(override, 10, 64)
		if err == nil {
			values.Proxy.UID = v
		}
	}

	if override, ok := annotations[k8s.ProxyEnableExternalProfilesAnnotation]; ok {
		value, err := strconv.ParseBool(override)
		if err == nil {
			values.Proxy.EnableExternalProfiles = value
		}
	}

	if override, ok := annotations[k8s.ProxyInitImageAnnotation]; ok {
		values.ProxyInit.Image.Name = override
	}

	if override, ok := annotations[k8s.ProxyImagePullPolicyAnnotation]; ok {
		values.ProxyInit.Image.PullPolicy = override
	}

	if override, ok := annotations[k8s.ProxyIgnoreInboundPortsAnnotation]; ok {
		values.ProxyInit.IgnoreInboundPorts = override
	}

	if override, ok := annotations[k8s.ProxyIgnoreOutboundPortsAnnotation]; ok {
		values.ProxyInit.IgnoreOutboundPorts = override
	}

	if override, ok := annotations[k8s.ProxyOpaquePortsAnnotation]; ok {
		var opaquePorts strings.Builder
		for _, pr := range conf.ParseOpaqueOverrides(override) {
			if opaquePorts.Len() > 0 {
				opaquePorts.WriteRune(',')
			}
			opaquePorts.WriteString(pr.ToString())
		}

		values.Proxy.OpaquePorts = opaquePorts.String()
	}

	if override, ok := annotations[k8s.DebugImageAnnotation]; ok {
		values.DebugContainer.Image.Name = override
	}

	if override, ok := annotations[k8s.DebugImageVersionAnnotation]; ok {
		values.DebugContainer.Image.Version = override
	}

	if override, ok := annotations[k8s.DebugImagePullPolicyAnnotation]; ok {
		values.DebugContainer.Image.PullPolicy = override
	}

	if override, ok := annotations[k8s.ProxyAwait]; ok {
		if override == k8s.Enabled || override == k8s.Disabled {
			values.Proxy.Await = override == k8s.Enabled
		} else {
			log.Warnf("unrecognized value used for the %s annotation, valid values are: [%s, %s]", k8s.ProxyAwait, k8s.Enabled, k8s.Disabled)
		}
	}

	if override, ok := annotations[k8s.ProxyDefaultInboundPolicyAnnotation]; ok {
		if override != k8s.AllUnauthenticated && override != k8s.AllAuthenticated && override != k8s.ClusterUnauthenticated && override != k8s.ClusterAuthenticated && override != k8s.Deny {
			log.Warnf("unrecognized value used for the %s annotation, valid values are: [%s, %s, %s, %s, %s]", k8s.ProxyDefaultInboundPolicyAnnotation, k8s.AllUnauthenticated, k8s.AllAuthenticated, k8s.ClusterUnauthenticated, k8s.ClusterAuthenticated, k8s.Deny)
		} else {
			values.Proxy.DefaultInboundPolicy = override
		}
	}

	if override, ok := annotations[k8s.ProxySkipSubnetsAnnotation]; ok {
		values.ProxyInit.SkipSubnets = override
	}

	if override, ok := annotations[k8s.ProxyAccessLogAnnotation]; ok {
		values.Proxy.AccessLog = override
	}
}
