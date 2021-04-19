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
	"github.com/linkerd/linkerd2/cli/flag"
	"github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/inject"
	"github.com/linkerd/linkerd2/pkg/k8s"
	api "github.com/linkerd/linkerd2/pkg/public"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

const (
	// for inject reports
	hostNetworkDesc                  = "pods do not use host networking"
	sidecarDesc                      = "pods do not have a 3rd party proxy or initContainer already injected"
	injectDisabledDesc               = "pods are not annotated to disable injection"
	unsupportedDesc                  = "at least one resource can be injected or annotated"
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
	defaults, err := charts.NewValues()
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}
	flags, proxyFlagSet := makeProxyFlags(defaults)
	injectFlags, injectFlagSet := makeInjectFlags(defaults)
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

			values := defaults
			if !ignoreCluster {
				values, err = fetchConfigs(cmd.Context())
				if err != nil {
					return err
				}
			}

			baseValues, err := values.DeepCopy()
			if err != nil {
				return err
			}
			err = flag.ApplySetFlags(values, append(flags, injectFlags...))
			if err != nil {
				return err
			}

			in, err := read(args[0])
			if err != nil {
				return err
			}

			overrideAnnotations := getOverrideAnnotations(values, baseValues)

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

	cmd.Flags().BoolVar(
		&manualOption, "manual", manualOption,
		"Include the proxy sidecar container spec in the YAML output (the auto-injector won't pick it up, so config annotations aren't supported) (default false)",
	)

	cmd.Flags().BoolVar(
		&ignoreCluster, "ignore-cluster", false,
		"Ignore the current Kubernetes cluster when checking for existing cluster configuration (default false)",
	)

	cmd.Flags().BoolVar(&enableDebugSidecar, "enable-debug-sidecar", enableDebugSidecar,
		"Inject a debug sidecar for data plane debugging")

	cmd.Flags().DurationVar(
		&closeWaitTimeout, "close-wait-timeout", closeWaitTimeout,
		"Sets nf_conntrack_tcp_timeout_close_wait")

	cmd.Flags().AddFlagSet(proxyFlagSet)
	cmd.Flags().AddFlagSet(injectFlagSet)

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

	if conf.IsService() {
		opaquePorts, ok := rt.overrideAnnotations[k8s.ProxyOpaquePortsAnnotation]
		if ok {
			annotations := map[string]string{k8s.ProxyOpaquePortsAnnotation: opaquePorts}
			bytes, err = conf.AnnotateService(annotations)
			report.Annotated = true
		}
		return bytes, reports, err
	}
	if rt.allowNsInject && conf.IsNamespace() {
		bytes, err = conf.AnnotateNamespace(rt.overrideAnnotations)
		report.Annotated = true
		return bytes, reports, err
	}
	if conf.HasPodTemplate() {
		conf.AppendPodAnnotations(rt.overrideAnnotations)
		report.Annotated = true
	}

	if ok, _ := report.Injectable(); !ok {
		if errs := report.ThrowInjectError(); len(errs) > 0 {
			return bytes, reports, fmt.Errorf("failed to inject %s%s%s: %v", report.Kind, slash, report.Name, concatErrors(errs, ", "))
		}
		return bytes, reports, nil
	}

	if rt.injectProxy {
		// delete the inject annotation if present as its not needed in the manual case
		// prevents injector from taking a different code path in the ignress mode
		delete(rt.overrideAnnotations, k8s.ProxyInjectAnnotation)
		conf.AppendPodAnnotation(k8s.CreatedByAnnotation, k8s.CreatedByAnnotationValue())
	} else if !rt.values.Proxy.IsIngress { // Add enabled annotation only if its not ingress mode to prevent overriding the annotation
		// flag the auto-injector to inject the proxy, regardless of the namespace annotation
		conf.AppendPodAnnotation(k8s.ProxyInjectAnnotation, k8s.ProxyInjectEnabled)
	}

	patchJSON, err := conf.GetPodPatch(rt.injectProxy)
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
	annotatable := false
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

		if r.IsAnnotatable() {
			annotatable = true
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

	if len(injected) == 0 && !annotatable {
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
		if r.Annotated {
			output.Write([]byte(fmt.Sprintf("%s \"%s\" annotated\n", r.Kind, r.Name)))
		}
		ok, _ := r.Injectable()
		if ok {
			output.Write([]byte(fmt.Sprintf("%s \"%s\" injected\n", r.Kind, r.Name)))
		}
		if !r.Annotated && !ok {
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

func fetchConfigs(ctx context.Context) (*linkerd2.Values, error) {

	api.CheckPublicAPIClientOrRetryOrExit(healthcheck.Options{
		ControlPlaneNamespace: controlPlaneNamespace,
		KubeConfig:            kubeconfigPath,
		Impersonate:           impersonate,
		ImpersonateGroup:      impersonateGroup,
		KubeContext:           kubeContext,
		APIAddr:               apiAddr,
		RetryDeadline:         time.Time{},
	})

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
func getOverrideAnnotations(values *charts.Values, base *charts.Values) map[string]string {
	overrideAnnotations := make(map[string]string)

	proxy := values.Proxy
	baseProxy := base.Proxy
	if proxy.Image.Version != baseProxy.Image.Version {
		overrideAnnotations[k8s.ProxyVersionOverrideAnnotation] = proxy.Image.Version
	}

	if values.ProxyInit.IgnoreInboundPorts != base.ProxyInit.IgnoreInboundPorts {
		overrideAnnotations[k8s.ProxyIgnoreInboundPortsAnnotation] = values.ProxyInit.IgnoreInboundPorts
	}
	if values.ProxyInit.IgnoreOutboundPorts != base.ProxyInit.IgnoreOutboundPorts {
		overrideAnnotations[k8s.ProxyIgnoreOutboundPortsAnnotation] = values.ProxyInit.IgnoreOutboundPorts
	}

	if proxy.Ports.Admin != baseProxy.Ports.Admin {
		overrideAnnotations[k8s.ProxyAdminPortAnnotation] = fmt.Sprintf("%d", proxy.Ports.Admin)
	}
	if proxy.Ports.Control != baseProxy.Ports.Control {
		overrideAnnotations[k8s.ProxyControlPortAnnotation] = fmt.Sprintf("%d", proxy.Ports.Control)
	}
	if proxy.Ports.Inbound != baseProxy.Ports.Inbound {
		overrideAnnotations[k8s.ProxyInboundPortAnnotation] = fmt.Sprintf("%d", proxy.Ports.Inbound)
	}
	if proxy.Ports.Outbound != baseProxy.Ports.Outbound {
		overrideAnnotations[k8s.ProxyOutboundPortAnnotation] = fmt.Sprintf("%d", proxy.Ports.Outbound)
	}
	if proxy.OpaquePorts != baseProxy.OpaquePorts {
		overrideAnnotations[k8s.ProxyOpaquePortsAnnotation] = proxy.OpaquePorts
	}

	if proxy.Image.Name != baseProxy.Image.Name {
		overrideAnnotations[k8s.ProxyImageAnnotation] = proxy.Image.Name
	}
	if values.ProxyInit.Image.Name != base.ProxyInit.Image.Name {
		overrideAnnotations[k8s.ProxyInitImageAnnotation] = values.ProxyInit.Image.Name
	}
	if values.DebugContainer.Image.Name != base.DebugContainer.Image.Name {
		overrideAnnotations[k8s.DebugImageAnnotation] = values.DebugContainer.Image.Name
	}

	if values.ProxyInit.Image.Version != base.ProxyInit.Image.Version {
		overrideAnnotations[k8s.ProxyInitImageVersionAnnotation] = values.ProxyInit.Image.Version
	}

	if values.DebugContainer.Image.Version != base.DebugContainer.Image.Version {
		overrideAnnotations[k8s.DebugImageVersionAnnotation] = values.DebugContainer.Image.Version
	}

	if proxy.Image.PullPolicy != baseProxy.Image.PullPolicy {
		overrideAnnotations[k8s.ProxyImagePullPolicyAnnotation] = proxy.Image.PullPolicy
	}

	if proxy.UID != baseProxy.UID {
		overrideAnnotations[k8s.ProxyUIDAnnotation] = strconv.FormatInt(proxy.UID, 10)
	}

	if proxy.LogLevel != baseProxy.LogLevel {
		overrideAnnotations[k8s.ProxyLogLevelAnnotation] = proxy.LogLevel
	}

	if proxy.LogFormat != baseProxy.LogFormat {
		overrideAnnotations[k8s.ProxyLogFormatAnnotation] = proxy.LogFormat
	}

	if proxy.DisableIdentity != baseProxy.DisableIdentity {
		overrideAnnotations[k8s.ProxyDisableIdentityAnnotation] = strconv.FormatBool(proxy.DisableIdentity)
	}

	if proxy.RequireIdentityOnInboundPorts != baseProxy.RequireIdentityOnInboundPorts {
		overrideAnnotations[k8s.ProxyRequireIdentityOnInboundPortsAnnotation] = proxy.RequireIdentityOnInboundPorts
	}

	if proxy.EnableExternalProfiles != baseProxy.EnableExternalProfiles {
		overrideAnnotations[k8s.ProxyEnableExternalProfilesAnnotation] = strconv.FormatBool(proxy.EnableExternalProfiles)
	}

	if proxy.IsIngress != baseProxy.IsIngress {
		overrideAnnotations[k8s.ProxyInjectAnnotation] = k8s.ProxyInjectIngress
	}

	if proxy.Resources.CPU.Request != baseProxy.Resources.CPU.Request {
		overrideAnnotations[k8s.ProxyCPURequestAnnotation] = proxy.Resources.CPU.Request
	}
	if proxy.Resources.CPU.Limit != baseProxy.Resources.CPU.Limit {
		overrideAnnotations[k8s.ProxyCPULimitAnnotation] = proxy.Resources.CPU.Limit
	}
	if proxy.Resources.Memory.Request != baseProxy.Resources.Memory.Request {
		overrideAnnotations[k8s.ProxyMemoryRequestAnnotation] = proxy.Resources.Memory.Request
	}
	if proxy.Resources.Memory.Limit != baseProxy.Resources.Memory.Limit {
		overrideAnnotations[k8s.ProxyMemoryLimitAnnotation] = proxy.Resources.Memory.Limit
	}
	if proxy.WaitBeforeExitSeconds != baseProxy.WaitBeforeExitSeconds {
		overrideAnnotations[k8s.ProxyWaitBeforeExitSecondsAnnotation] = uintToString(proxy.WaitBeforeExitSeconds)
	}

	if proxy.Await != baseProxy.Await {
		if proxy.Await {
			overrideAnnotations[k8s.ProxyAwait] = k8s.Enabled
		} else {
			overrideAnnotations[k8s.ProxyAwait] = k8s.Disabled
		}
	}

	// Set fields that can't be converted into annotations
	values.Namespace = controlPlaneNamespace

	return overrideAnnotations
}

func uintToString(v uint64) string {
	return strconv.FormatUint(v, 10)
}
