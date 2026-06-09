package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	valuespkg "helm.sh/helm/v3/pkg/cli/values"
	utilsexec "k8s.io/utils/exec"
)

type checkOptions struct {
	VersionOverride    string
	PreInstallOnly     bool
	CrdsOnly           bool
	DataPlaneOnly      bool
	Wait               time.Duration
	Namespace          string
	CniEnabled         bool
	Output             string
	CliVersionOverride string
}

var CheckOptions *checkOptions

func newCheckOptions() *checkOptions {
	return &checkOptions{
		VersionOverride:    "",
		PreInstallOnly:     false,
		CrdsOnly:           false,
		DataPlaneOnly:      false,
		Wait:               300 * time.Second,
		Namespace:          "",
		CniEnabled:         false,
		Output:             tableOutput,
		CliVersionOverride: "",
	}
}

// nonConfigFlagSet specifies flags not allowed with `linkerd check config`
func (options *checkOptions) nonConfigFlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("non-config-check", pflag.ExitOnError)

	flags.BoolVar(&options.CniEnabled, "linkerd-cni-enabled", options.CniEnabled, "When running pre-installation checks (--pre), assume the linkerd-cni plugin is already installed, and a NET_ADMIN check is not needed")
	flags.StringVarP(&options.Namespace, "namespace", "n", options.Namespace, "Namespace to use for --proxy checks (default: all namespaces)")
	flags.BoolVar(&options.PreInstallOnly, "pre", options.PreInstallOnly, "Only run pre-installation checks, to determine if the control plane can be installed")
	flags.BoolVar(&options.CrdsOnly, "crds", options.CrdsOnly, "Only run checks which determine if the Linkerd CRDs have been installed")
	flags.BoolVar(&options.DataPlaneOnly, "proxy", options.DataPlaneOnly, "Only run data-plane checks, to determine if the data plane is healthy")

	return flags
}

// checkFlagSet specifies flags allowed with and without `config`
func (options *checkOptions) checkFlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("check", pflag.ExitOnError)

	flags.StringVar(&options.VersionOverride, "expected-version", options.VersionOverride, "Overrides the version used when checking if Linkerd is running the latest version (mostly for testing)")
	flags.StringVar(&options.CliVersionOverride, "cli-version-override", "", "Used to override the version of the cli (mostly for testing)")
	flags.StringVarP(&options.Output, "output", "o", options.Output, "Output format. One of: table, json, short")
	flags.DurationVar(&options.Wait, "wait", options.Wait, "Maximum allowed time for all tests to pass")

	return flags
}

func (options *checkOptions) validate() error {
	if options.PreInstallOnly && options.DataPlaneOnly {
		return errors.New("--pre and --proxy flags are mutually exclusive")
	}
	if options.PreInstallOnly && options.CrdsOnly {
		return errors.New("--pre and --crds flags are mutually exclusive")
	}
	if !options.PreInstallOnly && options.CniEnabled {
		return errors.New("--linkerd-cni-enabled can only be used with --pre")
	}
	if options.Output != tableOutput && options.Output != jsonOutput && options.Output != shortOutput {
		return fmt.Errorf("Invalid output type '%s'. Supported output types are: %s, %s, %s", options.Output, jsonOutput, tableOutput, shortOutput)
	}
	return nil
}

func newCmdCheck() *cobra.Command {
	CheckOptions = newCheckOptions()
	checkFlags := CheckOptions.checkFlagSet()
	nonConfigFlags := CheckOptions.nonConfigFlagSet()

	cmd := &cobra.Command{
		Use:   "check [flags]",
		Args:  cobra.NoArgs,
		Short: "Check the Linkerd installation for potential problems",
		Long: `Check the Linkerd installation for potential problems.

The check command will perform a series of checks to validate that the linkerd
CLI and control plane are configured correctly. If the command encounters a
failure it will print additional information about the failure and exit with a
non-zero exit code.`,
		Example: `  # Check that the Linkerd control plane is up and running
  linkerd check

  # Check that the Linkerd control plane can be installed in the "test" namespace
  linkerd check --pre --linkerd-namespace test

  # Check that the Linkerd data plane proxies in the "app" namespace are up and running
  linkerd check --proxy --namespace app`,
		RunE: func(cmd *cobra.Command, args []string) error {
			hc, err := ConfigureChecks(cmd.Context(), CheckOptions)
			if err != nil {
				return err
			}
			return RunChecks(cmd, stdout, stderr, hc, CheckOptions)
		},
	}

	cmd.PersistentFlags().AddFlagSet(checkFlags)
	cmd.Flags().AddFlagSet(nonConfigFlags)

	pkgcmd.ConfigureNamespaceFlagCompletion(cmd, []string{"namespace"},
		kubeconfigPath, impersonate, impersonateGroup, kubeContext)

	return cmd
}

func ConfigureChecks(ctx context.Context, options *checkOptions) (*healthcheck.HealthChecker, error) {
	err := options.validate()
	if err != nil {
		return nil, fmt.Errorf("Validation error when executing check command: %w", err)
	}

	if options.CliVersionOverride != "" {
		version.Version = options.CliVersionOverride
	}

	checks := []healthcheck.CategoryID{
		healthcheck.KubernetesAPIChecks,
		healthcheck.KubernetesVersionChecks,
		healthcheck.LinkerdVersionChecks,
	}

	crdManifest := bytes.Buffer{}
	err = renderCRDs(ctx, nil, &crdManifest, valuespkg.Options{
		// GatewayAPI CRDs are optional so don't check for them.
		Values: []string{
			"installGatewayAPI=false",
		},
	}, "yaml")
	if err != nil {
		return nil, err
	}
	var installManifest string
	var values *charts.Values
	if options.PreInstallOnly {
		checks = append(checks, healthcheck.LinkerdPreInstallChecks)
		if options.CniEnabled {
			checks = append(checks, healthcheck.LinkerdCNIPluginChecks)
		}
		values, installManifest, err = renderInstallManifest(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error rendering install manifest: %s\n", err)
			os.Exit(1)
		}
	} else if options.CrdsOnly {
		checks = append(checks, healthcheck.LinkerdCRDChecks)
	} else {
		checks = append(checks, healthcheck.LinkerdConfigChecks)

		checks = append(checks, healthcheck.LinkerdControlPlaneExistenceChecks)
		checks = append(checks, healthcheck.LinkerdIdentity)
		checks = append(checks, healthcheck.LinkerdWebhooksAndAPISvcTLS)
		checks = append(checks, healthcheck.LinkerdControlPlaneProxyChecks)

		if options.DataPlaneOnly {
			checks = append(checks, healthcheck.LinkerdDataPlaneChecks)
			checks = append(checks, healthcheck.LinkerdIdentityDataPlane)
			checks = append(checks, healthcheck.LinkerdOpaquePortsDefinitionChecks)
		} else {
			checks = append(checks, healthcheck.LinkerdControlPlaneVersionChecks)
			checks = append(checks, healthcheck.LinkerdExtensionChecks)
		}
		checks = append(checks, healthcheck.LinkerdCNIPluginChecks)
		checks = append(checks, healthcheck.LinkerdHAChecks)
	}

	return healthcheck.NewHealthChecker(checks, &healthcheck.Options{
		IsMainCheckCommand:    true,
		ControlPlaneNamespace: controlPlaneNamespace,
		CNINamespace:          cniNamespace,
		DataPlaneNamespace:    options.Namespace,
		KubeConfig:            kubeconfigPath,
		KubeContext:           kubeContext,
		Impersonate:           impersonate,
		ImpersonateGroup:      impersonateGroup,
		APIAddr:               apiAddr,
		VersionOverride:       options.VersionOverride,
		RetryDeadline:         time.Now().Add(options.Wait),
		CNIEnabled:            options.CniEnabled,
		InstallManifest:       installManifest,
		CRDManifest:           crdManifest.String(),
		ChartValues:           values,
	}), nil
}

func RunChecks(cmd *cobra.Command, wout io.Writer, werr io.Writer, hc *healthcheck.HealthChecker, options *checkOptions) error {
	success, warning := healthcheck.RunChecks(wout, werr, hc, options.Output)

	if !options.PreInstallOnly && !options.CrdsOnly {
		extensionSuccess, extensionWarning, err := runExtensionChecks(cmd, wout, werr, options)
		if err != nil {
			fmt.Fprintf(werr, "Failed to run extensions checks: %s\n", err)
			os.Exit(1)
		}

		success = success && extensionSuccess
		warning = warning || extensionWarning
	}

	healthcheck.PrintChecksResult(wout, options.Output, success, warning)

	if !success {
		os.Exit(1)
	}

	return nil
}

func runExtensionChecks(cmd *cobra.Command, wout io.Writer, werr io.Writer, opts *checkOptions) (bool, bool, error) {
	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return false, false, err
	}

	namespaces, err := kubeAPI.GetAllNamespacesWithExtensionLabel(cmd.Context())
	if err != nil {
		return false, false, err
	}
	nsLabels := []string{}
	for _, ns := range namespaces {
		ext := ns.Labels[k8s.LinkerdExtensionLabel]
		nsLabels = append(nsLabels, ext)
	}

	exec := utilsexec.New()

	extensions, missing := findExtensions(os.Getenv("PATH"), filepath.Glob, exec, nsLabels)

	// no extensions to check
	if len(extensions) == 0 && len(missing) == 0 {
		return true, false, nil
	}

	extensionSuccess, extensionWarning := runExtensionsChecks(
		wout, werr, extensions, missing, exec, getExtensionCheckFlags(cmd.Flags()), opts.Output,
	)
	return extensionSuccess, extensionWarning, nil
}

func getExtensionCheckFlags(lf *pflag.FlagSet) []string {
	extensionFlags := []string{
		"api-addr", "context", "as", "as-group", "kubeconfig", "linkerd-namespace", "verbose",
		"namespace", "proxy", "wait",
	}
	cmdLineFlags := []string{}
	for _, flag := range extensionFlags {
		f := lf.Lookup(flag)
		if f != nil {
			val := f.Value.String()
			if val != "" {
				cmdLineFlags = append(cmdLineFlags, fmt.Sprintf("--%s=%s", f.Name, val))
			}
		}
	}
	cmdLineFlags = append(cmdLineFlags, "--output=json")
	return cmdLineFlags
}

func renderInstallManifest(ctx context.Context) (*charts.Values, string, error) {
	// Create the default values.
	k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 30*time.Second)
	if err != nil {
		return nil, "", err
	}
	values, err := charts.NewValues()
	if err != nil {
		return nil, "", err
	}
	err = initializeIssuerCredentials(ctx, k8sAPI, values)
	if err != nil {
		return nil, "", err
	}

	// Use empty valuesOverrides because there are no option values to merge.
	var b strings.Builder
	err = renderControlPlane(&b, values, map[string]interface{}{}, "yaml")
	if err != nil {
		return nil, "", err
	}
	return values, b.String(), nil
}
