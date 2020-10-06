package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/inject"
	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"sigs.k8s.io/yaml"
)

const (
	// for inject reports
	hostNetworkDesc                  = "pods do not use host networking"
	sidecarDesc                      = "pods do not have a 3rd party proxy or initContainer already injected"
	injectDisabledDesc               = "pods are not annotated to disable injection"
	unsupportedDesc                  = "at least one resource injected"
	udpDesc                          = "pod specs do not include UDP ports"
	automountServiceAccountTokenDesc = "pods do not have automountServiceAccountToken set to \"false\""
	slash                            = "/"
)

type resourceTransformerInject struct {
	allowNsInject       bool
	injectProxy         bool
	values              *linkerd2.Values
	overrideAnnotations map[string]string
	enableDebugSidecar  bool
	closeWaitTimeout    time.Duration
}

func runInjectCmd(inputs []io.Reader, errWriter, outWriter io.Writer, transformer *resourceTransformerInject) int {
	return transformInput(inputs, errWriter, outWriter, transformer)
}

func newCmdInject() *cobra.Command {
	options := &proxyConfigOptions{}
	var manualOption, enableDebugSidecar bool
	var closeWaitTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "inject [flags] CONFIG-FILE",
		Short: "Add the Linkerd proxy to a Kubernetes config",
		Long: `Add the Linkerd proxy to a Kubernetes config.

You can inject resources contained in a single file, inside a folder and its
sub-folders, or coming from stdin.`,
		Example: `  # Inject all the deployments in the default namespace.
  kubectl get deploy -o yaml | linkerd inject - | kubectl apply -f -

  # Injecting a file from a remote URL
  linkerd inject http://url.to/yml | kubectl apply -f -

  # Inject all the resources inside a folder and its sub-folders.
  linkerd inject <folder> | kubectl apply -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("please specify a kubernetes resource file")
			}

			if err := options.validate(); err != nil {
				return err
			}

			in, err := read(args[0])
			if err != nil {
				return err
			}

			values, err := options.fetchConfigsOrDefault(cmd.Context())
			if err != nil {
				return err
			}
			overrideAnnotations := map[string]string{}
			options.overrideConfigs(values, overrideAnnotations)

			transformer := &resourceTransformerInject{
				allowNsInject:       true,
				injectProxy:         manualOption,
				values:              values,
				overrideAnnotations: overrideAnnotations,
				enableDebugSidecar:  enableDebugSidecar,
				closeWaitTimeout:    closeWaitTimeout,
			}
			exitCode := uninjectAndInject(in, stderr, stdout, transformer)
			os.Exit(exitCode)
			return nil
		},
	}

	flags := options.flagSet(pflag.ExitOnError)
	flags.BoolVar(
		&manualOption, "manual", manualOption,
		"Include the proxy sidecar container spec in the YAML output (the auto-injector won't pick it up, so config annotations aren't supported) (default false)",
	)
	flags.Uint64Var(
		&options.waitBeforeExitSeconds, "wait-before-exit-seconds", options.waitBeforeExitSeconds,
		"The period during which the proxy sidecar must stay alive while its pod is terminating. "+
			"Must be smaller than terminationGracePeriodSeconds for the pod (default 0)",
	)
	flags.BoolVar(
		&options.disableIdentity, "disable-identity", options.disableIdentity,
		"Disables resources from participating in TLS identity",
	)

	flags.BoolVar(
		&options.disableTap, "disable-tap", options.disableTap,
		"Disables resources from being tapped",
	)

	flags.BoolVar(
		&options.ignoreCluster, "ignore-cluster", options.ignoreCluster,
		"Ignore the current Kubernetes cluster when checking for existing cluster configuration (default false)",
	)

	flags.BoolVar(&enableDebugSidecar, "enable-debug-sidecar", enableDebugSidecar,
		"Inject a debug sidecar for data plane debugging")

	flags.StringVar(&options.traceCollector, "trace-collector", options.traceCollector,
		"Collector Service address for the proxies to send Trace Data")

	flags.StringVar(&options.traceCollectorSvcAccount, "trace-collector-svc-account", options.traceCollectorSvcAccount,
		"Service account associated with the Trace collector instance")

	flags.StringSliceVar(&options.requireIdentityOnInboundPorts, "require-identity-on-inbound-ports", options.requireIdentityOnInboundPorts,
		"Inbound ports on which the proxy should require identity")

	flags.DurationVar(
		&closeWaitTimeout, "close-wait-timeout", closeWaitTimeout,
		"Sets nf_conntrack_tcp_timeout_close_wait")

	cmd.PersistentFlags().AddFlagSet(flags)

	return cmd
}

func uninjectAndInject(inputs []io.Reader, errWriter, outWriter io.Writer, transformer *resourceTransformerInject) int {
	var out bytes.Buffer
	if exitCode := runUninjectSilentCmd(inputs, errWriter, &out, transformer.values); exitCode != 0 {
		return exitCode
	}
	return runInjectCmd([]io.Reader{&out}, errWriter, outWriter, transformer)
}

func (rt resourceTransformerInject) transform(bytes []byte) ([]byte, []inject.Report, error) {
	conf := inject.NewResourceConfig(rt.values, inject.OriginCLI)

	if rt.enableDebugSidecar {
		conf.AppendPodAnnotation(k8s.ProxyEnableDebugAnnotation, "true")
	}

	if rt.closeWaitTimeout != time.Duration(0) {
		conf.AppendPodAnnotation(k8s.CloseWaitTimeoutAnnotation, rt.closeWaitTimeout.String())
	}

	report, err := conf.ParseMetaAndYAML(bytes)
	if err != nil {
		return nil, nil, err
	}

	if conf.IsControlPlaneComponent() && !rt.injectProxy {
		return nil, nil, errors.New("--manual must be set when injecting control plane components")
	}

	reports := []inject.Report{*report}

	if rt.allowNsInject && conf.IsNamespace() {
		b, err := conf.InjectNamespace(rt.overrideAnnotations)
		return b, reports, err
	}
	if b, _ := report.Injectable(); !b {
		if errs := report.ThrowInjectError(); len(errs) > 0 {
			return bytes, reports, fmt.Errorf("failed to inject %s%s%s: %v", report.Kind, slash, report.Name, concatErrors(errs, ", "))
		}
		return bytes, reports, nil
	}

	if rt.injectProxy {
		conf.AppendPodAnnotation(k8s.CreatedByAnnotation, k8s.CreatedByAnnotationValue())
	} else {
		// flag the auto-injector to inject the proxy, regardless of the namespace annotation
		conf.AppendPodAnnotation(k8s.ProxyInjectAnnotation, k8s.ProxyInjectEnabled)
	}

	if len(rt.overrideAnnotations) > 0 {
		conf.AppendPodAnnotations(rt.overrideAnnotations)
	}

	patchJSON, err := conf.GetPatch(rt.injectProxy)
	if err != nil {
		return nil, nil, err
	}
	if len(patchJSON) == 0 {
		return bytes, reports, nil
	}
	log.Infof("patch generated for: %s", report.ResName())
	log.Debugf("patch: %s", patchJSON)
	patch, err := jsonpatch.DecodePatch(patchJSON)
	if err != nil {
		return nil, nil, err
	}
	origJSON, err := yaml.YAMLToJSON(bytes)
	if err != nil {
		return nil, nil, err
	}
	injectedJSON, err := patch.Apply(origJSON)
	if err != nil {
		return nil, nil, err
	}
	injectedYAML, err := conf.JSONToYAML(injectedJSON)
	if err != nil {
		return nil, nil, err
	}
	return injectedYAML, reports, nil
}

func (resourceTransformerInject) generateReport(reports []inject.Report, output io.Writer) {
	injected := []inject.Report{}
	hostNetwork := []string{}
	sidecar := []string{}
	udp := []string{}
	injectDisabled := []string{}
	automountServiceAccountTokenFalse := []string{}
	warningsPrinted := verbose

	for _, r := range reports {
		if b, _ := r.Injectable(); b {
			injected = append(injected, r)
		}

		if r.HostNetwork {
			hostNetwork = append(hostNetwork, r.ResName())
			warningsPrinted = true
		}

		if r.Sidecar {
			sidecar = append(sidecar, r.ResName())
			warningsPrinted = true
		}

		if r.UDP {
			udp = append(udp, r.ResName())
			warningsPrinted = true
		}

		if r.InjectDisabled {
			injectDisabled = append(injectDisabled, r.ResName())
			warningsPrinted = true
		}

		if !r.AutomountServiceAccountToken {
			automountServiceAccountTokenFalse = append(automountServiceAccountTokenFalse, r.ResName())
			warningsPrinted = true
		}
	}

	//
	// Warnings
	//

	// Leading newline to separate from yaml output on stdout
	output.Write([]byte("\n"))

	if len(hostNetwork) > 0 {
		output.Write([]byte(fmt.Sprintf("%s \"hostNetwork: true\" detected in %s\n", warnStatus, strings.Join(hostNetwork, ", "))))
	} else if verbose {
		output.Write([]byte(fmt.Sprintf("%s %s\n", okStatus, hostNetworkDesc)))
	}

	if len(sidecar) > 0 {
		output.Write([]byte(fmt.Sprintf("%s known 3rd party sidecar detected in %s\n", warnStatus, strings.Join(sidecar, ", "))))
	} else if verbose {
		output.Write([]byte(fmt.Sprintf("%s %s\n", okStatus, sidecarDesc)))
	}

	if len(injectDisabled) > 0 {
		output.Write([]byte(fmt.Sprintf("%s \"%s: %s\" annotation set on %s\n",
			warnStatus, k8s.ProxyInjectAnnotation, k8s.ProxyInjectDisabled, strings.Join(injectDisabled, ", "))))
	} else if verbose {
		output.Write([]byte(fmt.Sprintf("%s %s\n", okStatus, injectDisabledDesc)))
	}

	if len(injected) == 0 {
		output.Write([]byte(fmt.Sprintf("%s no supported objects found\n", warnStatus)))
		warningsPrinted = true
	} else if verbose {
		output.Write([]byte(fmt.Sprintf("%s %s\n", okStatus, unsupportedDesc)))
	}

	if len(udp) > 0 {
		verb := "uses"
		if len(udp) > 1 {
			verb = "use"
		}
		output.Write([]byte(fmt.Sprintf("%s %s %s \"protocol: UDP\"\n", warnStatus, strings.Join(udp, ", "), verb)))
	} else if verbose {
		output.Write([]byte(fmt.Sprintf("%s %s\n", okStatus, udpDesc)))
	}

	if len(automountServiceAccountTokenFalse) == 0 && verbose {
		output.Write([]byte(fmt.Sprintf("%s %s\n", okStatus, automountServiceAccountTokenDesc)))
	}
	//
	// Summary
	//
	if warningsPrinted {
		output.Write([]byte("\n"))
	}

	for _, r := range reports {
		if b, _ := r.Injectable(); b {
			output.Write([]byte(fmt.Sprintf("%s \"%s\" injected\n", r.Kind, r.Name)))
		} else {
			if r.Kind != "" {
				output.Write([]byte(fmt.Sprintf("%s \"%s\" skipped\n", r.Kind, r.Name)))
			} else {
				output.Write([]byte(fmt.Sprintln("document missing \"kind\" field, skipped")))
			}
		}
	}

	// Trailing newline to separate from kubectl output if piping
	output.Write([]byte("\n"))
}

func (options *proxyConfigOptions) fetchConfigsOrDefault(ctx context.Context) (*linkerd2.Values, error) {
	if options.ignoreCluster {
		if !options.disableIdentity {
			return nil, errors.New("--disable-identity must be set with --ignore-cluster")
		}

		return linkerd2.NewValues(false)
	}

	checkPublicAPIClientOrExit()
	api, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return nil, err
	}

	// Get the New Linkerd Configuration
	_, values, err := healthcheck.FetchCurrentConfiguration(ctx, api, controlPlaneNamespace)
	return values, err
}

// overrideConfigs uses command-line overrides to update the provided configs.
// the overrideAnnotations map keeps track of which configs are overridden, by
// storing the corresponding annotations and values.
func (options *proxyConfigOptions) overrideConfigs(values *linkerd2.Values, overrideAnnotations map[string]string) {
	if options.proxyVersion != "" {
		overrideAnnotations[k8s.ProxyVersionOverrideAnnotation] = options.proxyVersion
	}

	if len(options.ignoreInboundPorts) > 0 {
		overrideAnnotations[k8s.ProxyIgnoreInboundPortsAnnotation] = strings.Join(options.ignoreInboundPorts, ",")
	}
	if len(options.ignoreOutboundPorts) > 0 {
		overrideAnnotations[k8s.ProxyIgnoreOutboundPortsAnnotation] = strings.Join(options.ignoreOutboundPorts, ",")
	}

	if options.proxyAdminPort != 0 {
		overrideAnnotations[k8s.ProxyAdminPortAnnotation] = fmt.Sprint(options.proxyAdminPort)
	}
	if options.proxyControlPort != 0 {
		overrideAnnotations[k8s.ProxyControlPortAnnotation] = fmt.Sprint(options.proxyControlPort)
	}
	if options.proxyInboundPort != 0 {
		overrideAnnotations[k8s.ProxyInboundPortAnnotation] = fmt.Sprint(options.proxyInboundPort)
	}
	if options.proxyOutboundPort != 0 {
		overrideAnnotations[k8s.ProxyOutboundPortAnnotation] = fmt.Sprint(options.proxyOutboundPort)
	}

	if options.dockerRegistry != "" {
		debugImage := values.DebugContainer.Image.Name
		if debugImage == "" {
			debugImage = k8s.DebugSidecarImage
		}
		overrideAnnotations[k8s.ProxyImageAnnotation] = overwriteRegistry(values.Global.Proxy.Image.Name, options.dockerRegistry)
		overrideAnnotations[k8s.ProxyInitImageAnnotation] = overwriteRegistry(values.Global.ProxyInit.Image.Name, options.dockerRegistry)
		overrideAnnotations[k8s.DebugImageAnnotation] = overwriteRegistry(debugImage, options.dockerRegistry)
	}

	if options.proxyImage != "" {
		overrideAnnotations[k8s.ProxyImageAnnotation] = options.proxyImage
	}

	if options.initImage != "" {
		overrideAnnotations[k8s.ProxyInitImageAnnotation] = options.initImage
	}

	if options.initImageVersion != "" {
		overrideAnnotations[k8s.ProxyInitImageVersionAnnotation] = options.initImageVersion
	}

	if options.debugImageVersion != "" {
		overrideAnnotations[k8s.DebugImageVersionAnnotation] = options.debugImageVersion
	}

	if options.imagePullPolicy != "" {
		overrideAnnotations[k8s.ProxyImagePullPolicyAnnotation] = options.imagePullPolicy
	}

	if options.proxyUID != 0 {
		overrideAnnotations[k8s.ProxyUIDAnnotation] = strconv.FormatInt(options.proxyUID, 10)
	}

	if options.proxyLogLevel != "" {
		overrideAnnotations[k8s.ProxyLogLevelAnnotation] = options.proxyLogLevel
	}

	if options.proxyLogFormat != "" {
		overrideAnnotations[k8s.ProxyLogFormatAnnotation] = options.proxyLogFormat
	}

	if options.disableIdentity {
		overrideAnnotations[k8s.ProxyDisableIdentityAnnotation] = strconv.FormatBool(true)
	}

	if len(options.requireIdentityOnInboundPorts) > 0 {
		overrideAnnotations[k8s.ProxyRequireIdentityOnInboundPortsAnnotation] = strings.Join(options.requireIdentityOnInboundPorts, ",")
	}

	if options.disableTap {
		overrideAnnotations[k8s.ProxyDisableTapAnnotation] = strconv.FormatBool(true)
	}

	// keep track of this option because its true/false value results in different
	// values being assigned to the LINKERD2_PROXY_DESTINATION_PROFILE_SUFFIXES
	// env var. Its annotation is added only if its value is true.
	if options.enableExternalProfiles {
		overrideAnnotations[k8s.ProxyEnableExternalProfilesAnnotation] = strconv.FormatBool(true)
	}

	if options.proxyCPURequest != "" {
		overrideAnnotations[k8s.ProxyCPURequestAnnotation] = options.proxyCPURequest
	}
	if options.proxyCPULimit != "" {
		overrideAnnotations[k8s.ProxyCPULimitAnnotation] = options.proxyCPULimit
	}
	if options.proxyMemoryRequest != "" {
		overrideAnnotations[k8s.ProxyMemoryRequestAnnotation] = options.proxyMemoryRequest
	}
	if options.proxyMemoryLimit != "" {
		values.Global.Proxy.Resources.Memory.Limit = options.proxyMemoryLimit
		overrideAnnotations[k8s.ProxyMemoryLimitAnnotation] = options.proxyMemoryLimit
	}

	if options.traceCollector != "" {
		overrideAnnotations[k8s.ProxyTraceCollectorSvcAddrAnnotation] = options.traceCollector
	}

	if options.traceCollectorSvcAccount != "" {
		overrideAnnotations[k8s.ProxyTraceCollectorSvcAccountAnnotation] = options.traceCollectorSvcAccount
	}
	if options.waitBeforeExitSeconds != 0 {
		overrideAnnotations[k8s.ProxyWaitBeforeExitSecondsAnnotation] = uintToString(options.waitBeforeExitSeconds)
	}

	// Set fields that can't be converted into annotations
	values.Global.Namespace = controlPlaneNamespace
}

func uintToString(v uint64) string {
	return strconv.FormatUint(v, 10)
}

// overwriteRegistry replaces the registry-portion of the provided image with the provided registry.
func overwriteRegistry(image, newRegistry string) string {
	if image == "" {
		return image
	}
	registry := newRegistry
	if registry != "" && !strings.HasSuffix(registry, slash) {
		registry += slash
	}
	imageName := image
	if strings.Contains(image, slash) {
		imageName = image[strings.LastIndex(image, slash)+1:]
	}
	return registry + imageName
}
