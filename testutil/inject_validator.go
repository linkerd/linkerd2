package testutil

import (
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/linkerd/linkerd2/pkg/k8s"
)

const enabled = "true"
const proxyContainerName = "linkerd-proxy"
const initContainerName = "linkerd-init"
const debugContainerName = "linkerd-debug"

// InjectValidator is used as a helper to generate
// correct injector flags and annotations and verify
// injected pods
type InjectValidator struct {
	NoInitContainer        bool
	DisableIdentity        bool
	AutoInject             bool
	AdminPort              int
	ControlPort            int
	EnableDebug            bool
	EnableExternalProfiles bool
	ImagePullPolicy        string
	InboundPort            int
	InitImage              string
	InitImageVersion       string
	OutboundPort           int
	CPULimit               string
	CPURequest             string
	MemoryLimit            string
	MemoryRequest          string
	Image                  string
	LogLevel               string
	LogFormat              string
	UID                    int
	Version                string
	RequireIdentityOnPorts string
	SkipOutboundPorts      string
	OpaquePorts            string
	SkipInboundPorts       string
	OutboundConnectTimeout string
	InboundConnectTimeout  string
	WaitBeforeExitSeconds  int
}

func (iv *InjectValidator) getContainer(pod *v1.PodSpec, name string, isInit bool) *v1.Container {
	containers := pod.Containers
	if isInit {
		containers = pod.InitContainers
	}
	for _, container := range containers {
		if container.Name == name {
			return &container
		}
	}
	return nil
}

func (iv *InjectValidator) validateEnvVar(container *v1.Container, envName, expectedValue string) error {
	for _, env := range container.Env {
		if env.Name == envName {
			if env.Value == expectedValue {
				return nil
			}
			return fmt.Errorf("env: %s, expected: %s, actual %s", envName, expectedValue, env.Value)
		}

	}
	return fmt.Errorf("cannot find env: %s", envName)
}

func (iv *InjectValidator) validatePort(container *v1.Container, portName string, expectedValue int) error {
	for _, port := range container.Ports {
		if port.Name == portName {
			if port.ContainerPort == int32(expectedValue) {
				return nil
			}
			return fmt.Errorf("port: %s, expected: %d, actual %d", portName, expectedValue, port.ContainerPort)
		}

	}
	return fmt.Errorf("cannot find port: %s", portName)
}

func (iv *InjectValidator) validateDebugContainer(pod *v1.PodSpec) error {
	if iv.EnableDebug {
		proxyContainer := iv.getContainer(pod, debugContainerName, false)
		if proxyContainer == nil {
			return fmt.Errorf("container %s missing", debugContainerName)
		}
	}
	return nil
}

func (iv *InjectValidator) validateProxyContainer(pod *v1.PodSpec) error {
	proxyContainer := iv.getContainer(pod, proxyContainerName, false)
	if proxyContainer == nil {
		return fmt.Errorf("container %s missing", proxyContainerName)
	}

	if iv.AdminPort != 0 {
		if err := iv.validateEnvVar(proxyContainer, "LINKERD2_PROXY_ADMIN_LISTEN_ADDR", fmt.Sprintf("0.0.0.0:%d", iv.AdminPort)); err != nil {
			return err
		}
		if proxyContainer.LivenessProbe.HTTPGet.Port.IntVal != int32(iv.AdminPort) {
			return fmt.Errorf("livenessProbe: expected: %d, actual %d", iv.AdminPort, proxyContainer.LivenessProbe.HTTPGet.Port.IntVal)
		}
		if proxyContainer.ReadinessProbe.HTTPGet.Port.IntVal != int32(iv.AdminPort) {
			return fmt.Errorf("readinessProbe: expected: %d, actual %d", iv.AdminPort, proxyContainer.LivenessProbe.HTTPGet.Port.IntVal)
		}

		if err := iv.validatePort(proxyContainer, "linkerd-admin", iv.AdminPort); err != nil {
			return err
		}
	}

	if iv.ControlPort != 0 {
		if err := iv.validateEnvVar(proxyContainer, "LINKERD2_PROXY_CONTROL_LISTEN_ADDR", fmt.Sprintf("0.0.0.0:%d", iv.ControlPort)); err != nil {
			return err
		}
	}

	if iv.EnableExternalProfiles {
		if err := iv.validateEnvVar(proxyContainer, "LINKERD2_PROXY_DESTINATION_PROFILE_SUFFIXES", "."); err != nil {
			return err
		}
	}

	if iv.ImagePullPolicy != "" {
		if string(proxyContainer.ImagePullPolicy) != iv.ImagePullPolicy {
			return fmt.Errorf("pullPolicy: expected: %s, actual %s", iv.ImagePullPolicy, string(proxyContainer.ImagePullPolicy))
		}
	}

	if iv.InboundPort != 0 {
		if err := iv.validateEnvVar(proxyContainer, "LINKERD2_PROXY_INBOUND_LISTEN_ADDR", fmt.Sprintf("0.0.0.0:%d", iv.InboundPort)); err != nil {
			return err
		}
		if proxyContainer.LivenessProbe.HTTPGet.Port.IntVal != int32(iv.AdminPort) {
			return fmt.Errorf("livenessProbe: expected: %d, actual %d", iv.AdminPort, proxyContainer.LivenessProbe.HTTPGet.Port.IntVal)
		}
		if proxyContainer.ReadinessProbe.HTTPGet.Port.IntVal != int32(iv.AdminPort) {
			return fmt.Errorf("readinessProbe: expected: %d, actual %d", iv.AdminPort, proxyContainer.LivenessProbe.HTTPGet.Port.IntVal)
		}
		if err := iv.validatePort(proxyContainer, "linkerd-proxy", iv.InboundPort); err != nil {
			return err
		}
	}

	if iv.OutboundPort != 0 {
		if err := iv.validateEnvVar(proxyContainer, "LINKERD2_PROXY_OUTBOUND_LISTEN_ADDR", fmt.Sprintf("127.0.0.1:%d", iv.OutboundPort)); err != nil {
			return err
		}
	}

	if iv.CPULimit != "" {
		limit := resource.MustParse(iv.CPULimit)
		if proxyContainer.Resources.Limits.Cpu() != nil {
			if !proxyContainer.Resources.Limits.Cpu().Equal(limit) {
				return fmt.Errorf("CpuLimit: expected %v, actual %v", &limit, proxyContainer.Resources.Limits.Cpu())
			}
		} else {
			return fmt.Errorf("CpuLimit: expected %v, but none", &limit)
		}

	}

	if iv.CPURequest != "" {
		request := resource.MustParse(iv.CPURequest)
		if proxyContainer.Resources.Requests.Cpu() != nil {
			if !proxyContainer.Resources.Requests.Cpu().Equal(request) {
				return fmt.Errorf("CpuRequest: expected %v, actual %v", &request, proxyContainer.Resources.Requests.Cpu())
			}
		} else {
			return fmt.Errorf("CpuRequest: expected %v, but none", &request)
		}
	}

	if iv.MemoryLimit != "" {
		limit := resource.MustParse(iv.MemoryLimit)
		if proxyContainer.Resources.Limits.Memory() != nil {
			if !proxyContainer.Resources.Limits.Memory().Equal(limit) {
				return fmt.Errorf("MemLimit: expected %v, actual %v", &limit, proxyContainer.Resources.Limits.Memory())
			}
		} else {
			return fmt.Errorf("MemLimit: expected %v, but none", &limit)
		}
	}

	if iv.MemoryRequest != "" {
		request := resource.MustParse(iv.MemoryRequest)
		if proxyContainer.Resources.Requests.Memory() != nil {
			if !proxyContainer.Resources.Requests.Memory().Equal(request) {
				return fmt.Errorf("MemRequest: expected %v, actual %v", &request, proxyContainer.Resources.Requests.Memory())
			}
		} else {
			return fmt.Errorf("MemRequest: expected %v, but none", &request)
		}
	}

	if iv.Image != "" || iv.Version != "" {
		image := strings.Split(proxyContainer.Image, ":")

		if len(image) != 2 {
			return fmt.Errorf("invalid proxy container image string: %s", proxyContainer.Image)
		}

		if iv.Image != "" {
			if image[0] != iv.Image {
				return fmt.Errorf("proxyImage: expected %s, actual %s", iv.Image, image[0])
			}
		}

		if iv.Version != "" {
			if image[1] != iv.Version {
				return fmt.Errorf("proxyImageVersion: expected %s, actual %s", iv.Version, image[1])
			}
		}
	}

	if iv.LogLevel != "" {
		if err := iv.validateEnvVar(proxyContainer, "LINKERD2_PROXY_LOG", iv.LogLevel); err != nil {
			return err
		}
	}

	if iv.LogFormat != "" {
		if err := iv.validateEnvVar(proxyContainer, "LINKERD2_PROXY_LOG_FORMAT", iv.LogFormat); err != nil {
			return err
		}
	}

	if iv.UID != 0 {
		if proxyContainer.SecurityContext.RunAsUser == nil {
			return fmt.Errorf("no RunAsUser specified")
		}
		if *proxyContainer.SecurityContext.RunAsUser != int64(iv.UID) {
			return fmt.Errorf("runAsUser: expected %d, actual %d", iv.UID, *proxyContainer.SecurityContext.RunAsUser)
		}
	}

	if iv.RequireIdentityOnPorts != "" {
		if err := iv.validateEnvVar(proxyContainer, "LINKERD2_PROXY_INBOUND_PORTS_REQUIRE_IDENTITY", iv.RequireIdentityOnPorts); err != nil {
			return err
		}
	}

	if iv.OpaquePorts != "" {
		if err := iv.validateEnvVar(proxyContainer, "LINKERD2_PROXY_INBOUND_PORTS_DISABLE_PROTOCOL_DETECTION", iv.OpaquePorts); err != nil {
			return err
		}
	}

	if iv.OutboundConnectTimeout != "" {
		if err := iv.validateEnvVar(proxyContainer, "LINKERD2_PROXY_OUTBOUND_CONNECT_TIMEOUT", iv.OutboundConnectTimeout); err != nil {
			return err
		}
	}

	if iv.OutboundConnectTimeout != "" {
		if err := iv.validateEnvVar(proxyContainer, "LINKERD2_PROXY_INBOUND_CONNECT_TIMEOUT", iv.InboundConnectTimeout); err != nil {
			return err
		}
	}

	if iv.WaitBeforeExitSeconds != 0 {
		expectedCmd := fmt.Sprintf("/bin/bash,-c,sleep %d", iv.WaitBeforeExitSeconds)
		actual := strings.Join(proxyContainer.Lifecycle.PreStop.Exec.Command, ",")
		if expectedCmd != strings.Join(proxyContainer.Lifecycle.PreStop.Exec.Command, ",") {
			return fmt.Errorf("preStopHook: expected %s, actual %s", expectedCmd, actual)
		}
	}

	return nil
}

func (iv *InjectValidator) validateInitContainer(pod *v1.PodSpec) error {
	if iv.NoInitContainer {
		return nil
	}
	initContainer := iv.getContainer(pod, initContainerName, true)
	if initContainer == nil {
		return fmt.Errorf("container %s missing", initContainerName)
	}

	if iv.InitImage != "" || iv.InitImageVersion != "" {

		image := strings.Split(initContainer.Image, ":")

		if len(image) != 2 {
			return fmt.Errorf("invalid proxy init image string: %s", initContainer.Image)
		}

		if iv.InitImage != "" {
			if image[0] != iv.InitImage {
				return fmt.Errorf("proxyInitImage: expected %s, actual %s", iv.InitImage, image[0])
			}
		}

		if iv.InitImageVersion != "" {
			if image[1] != iv.InitImageVersion {
				return fmt.Errorf("proxyInitImageVersion: expected %s, actual %s", iv.InitImageVersion, image[1])
			}
		}
	}

	if iv.InboundPort != 0 {
		if err := iv.validateArg(initContainer, "--incoming-proxy-port", strconv.Itoa(iv.InboundPort)); err != nil {
			return err
		}
	}

	if iv.OutboundPort != 0 {
		if err := iv.validateArg(initContainer, "--proxy-uid", strconv.Itoa(iv.UID)); err != nil {
			return err
		}
	}

	if iv.UID != 0 {
		if err := iv.validateArg(initContainer, "--outgoing-proxy-port", strconv.Itoa(iv.OutboundPort)); err != nil {
			return err
		}
	}

	if iv.SkipInboundPorts != "" {
		expectedPorts := fmt.Sprintf("%d,%d,%s", iv.ControlPort, iv.AdminPort, iv.SkipInboundPorts)
		if err := iv.validateArg(initContainer, "--inbound-ports-to-ignore", expectedPorts); err != nil {
			return err
		}
	}

	if iv.SkipOutboundPorts != "" {
		if err := iv.validateArg(initContainer, "--outbound-ports-to-ignore", iv.SkipOutboundPorts); err != nil {
			return err
		}
	}

	return nil
}

func (iv *InjectValidator) validateArg(container *v1.Container, argName, expectedValue string) error {
	for i, arg := range container.Args {
		if arg == argName {
			if len(container.Args) < i+2 {
				return fmt.Errorf("No value for arg %s", argName)
			}
			if container.Args[i+1] != expectedValue {
				return fmt.Errorf("container arg %s expected: %s, actual %s", argName, expectedValue, container.Args[i+1])
			}
			return nil
		}
	}

	return fmt.Errorf("Could not find arg: %s", argName)

}

// ValidatePod validates that the pod had been configured
// according by the injector correctly
func (iv *InjectValidator) ValidatePod(pod *v1.PodSpec) error {

	if err := iv.validateProxyContainer(pod); err != nil {
		return err
	}

	if err := iv.validateInitContainer(pod); err != nil {
		return err
	}

	if err := iv.validateDebugContainer(pod); err != nil {
		return err
	}

	return nil
}

// GetFlagsAndAnnotations retrieves the injector config flags and annotations
// based on the options provided
func (iv *InjectValidator) GetFlagsAndAnnotations() ([]string, map[string]string) {
	annotations := make(map[string]string)
	var flags []string

	if iv.AutoInject {
		annotations[k8s.ProxyInjectAnnotation] = k8s.ProxyInjectEnabled
	}

	if iv.AdminPort != 0 {
		annotations[k8s.ProxyAdminPortAnnotation] = strconv.Itoa(iv.AdminPort)
		flags = append(flags, fmt.Sprintf("--admin-port=%s", strconv.Itoa(iv.AdminPort)))
	}

	if iv.ControlPort != 0 {
		annotations[k8s.ProxyControlPortAnnotation] = strconv.Itoa(iv.ControlPort)
		flags = append(flags, fmt.Sprintf("--control-port=%s", strconv.Itoa(iv.ControlPort)))
	}

	if iv.DisableIdentity {
		annotations[k8s.IdentityModeDisabled] = enabled
		flags = append(flags, "--disable-identity")
	}

	if iv.EnableDebug {
		annotations[k8s.ProxyEnableDebugAnnotation] = enabled
		flags = append(flags, "--enable-debug-sidecar")
	}

	if iv.EnableExternalProfiles {
		annotations[k8s.ProxyEnableExternalProfilesAnnotation] = enabled
		flags = append(flags, "--enable-external-profiles")
	}

	if iv.ImagePullPolicy != "" {
		annotations[k8s.ProxyImagePullPolicyAnnotation] = iv.ImagePullPolicy
		flags = append(flags, fmt.Sprintf("--image-pull-policy=%s", iv.ImagePullPolicy))
	}

	if iv.InboundPort != 0 {
		annotations[k8s.ProxyInboundPortAnnotation] = strconv.Itoa(iv.InboundPort)
		flags = append(flags, fmt.Sprintf("--inbound-port=%s", strconv.Itoa(iv.InboundPort)))
	}

	if iv.InitImage != "" {
		annotations[k8s.ProxyInitImageAnnotation] = iv.InitImage
		flags = append(flags, fmt.Sprintf("--init-image=%s", iv.InitImage))
	}

	if iv.InitImageVersion != "" {
		annotations[k8s.ProxyInitImageVersionAnnotation] = iv.InitImageVersion
		flags = append(flags, fmt.Sprintf("--init-image-version=%s", iv.InitImageVersion))
	}

	if iv.OutboundPort != 0 {
		annotations[k8s.ProxyOutboundPortAnnotation] = strconv.Itoa(iv.OutboundPort)
		flags = append(flags, fmt.Sprintf("--outbound-port=%s", strconv.Itoa(iv.OutboundPort)))
	}

	if iv.CPULimit != "" {
		annotations[k8s.ProxyCPULimitAnnotation] = iv.CPULimit
		flags = append(flags, fmt.Sprintf("--proxy-cpu-limit=%s", iv.CPULimit))
	}

	if iv.CPURequest != "" {
		annotations[k8s.ProxyCPURequestAnnotation] = iv.CPURequest
		flags = append(flags, fmt.Sprintf("--proxy-cpu-request=%s", iv.CPURequest))
	}

	if iv.MemoryLimit != "" {
		annotations[k8s.ProxyMemoryLimitAnnotation] = iv.MemoryLimit
		flags = append(flags, fmt.Sprintf("--proxy-memory-limit=%s", iv.MemoryLimit))
	}

	if iv.MemoryRequest != "" {
		annotations[k8s.ProxyMemoryRequestAnnotation] = iv.MemoryRequest
		flags = append(flags, fmt.Sprintf("--proxy-memory-request=%s", iv.MemoryRequest))
	}

	if iv.Image != "" {
		annotations[k8s.ProxyImageAnnotation] = iv.Image
		flags = append(flags, fmt.Sprintf("--proxy-image=%s", iv.Image))
	}

	if iv.LogLevel != "" {
		annotations[k8s.ProxyLogLevelAnnotation] = iv.LogLevel
		flags = append(flags, fmt.Sprintf("--proxy-log-level=%s", iv.LogLevel))
	}

	if iv.LogFormat != "" {
		annotations[k8s.ProxyLogFormatAnnotation] = iv.LogFormat
	}

	if iv.UID != 0 {
		annotations[k8s.ProxyUIDAnnotation] = strconv.Itoa(iv.UID)
		flags = append(flags, fmt.Sprintf("--proxy-uid=%s", strconv.Itoa(iv.UID)))
	}

	if iv.Version != "" {
		annotations[k8s.ProxyVersionOverrideAnnotation] = iv.Version
		flags = append(flags, fmt.Sprintf("--proxy-version=%s", iv.Version))
	}

	if iv.RequireIdentityOnPorts != "" {
		annotations[k8s.ProxyRequireIdentityOnInboundPortsAnnotation] = iv.RequireIdentityOnPorts
		flags = append(flags, fmt.Sprintf("--require-identity-on-inbound-ports =%s", iv.RequireIdentityOnPorts))
	}

	if iv.SkipInboundPorts != "" {
		annotations[k8s.ProxyIgnoreInboundPortsAnnotation] = iv.SkipInboundPorts
		flags = append(flags, fmt.Sprintf("--skip-inbound-ports=%s", iv.SkipInboundPorts))
	}

	if iv.OpaquePorts != "" {
		annotations[k8s.ProxyOpaquePortsAnnotation] = iv.OpaquePorts
	}

	if iv.SkipOutboundPorts != "" {
		annotations[k8s.ProxyIgnoreOutboundPortsAnnotation] = iv.SkipOutboundPorts
		flags = append(flags, fmt.Sprintf("--skip-outbound-ports=%s", iv.SkipOutboundPorts))
	}

	if iv.OutboundConnectTimeout != "" {
		annotations[k8s.ProxyOutboundConnectTimeout] = iv.OutboundConnectTimeout
	}

	if iv.InboundConnectTimeout != "" {
		annotations[k8s.ProxyInboundConnectTimeout] = iv.InboundConnectTimeout
	}

	if iv.WaitBeforeExitSeconds != 0 {
		annotations[k8s.ProxyWaitBeforeExitSecondsAnnotation] = strconv.Itoa(iv.WaitBeforeExitSeconds)
		flags = append(flags, fmt.Sprintf("--wait-before-exit-secondst=%s", strconv.Itoa(iv.WaitBeforeExitSeconds)))

	}

	return flags, annotations
}
