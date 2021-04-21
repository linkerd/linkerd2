package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/linkerd/linkerd2/cli/flag"
	jaegerCmd "github.com/linkerd/linkerd2/jaeger/cmd"
	mcCmd "github.com/linkerd/linkerd2/multicluster/cmd"
	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	vizHealthCheck "github.com/linkerd/linkerd2/viz/pkg/healthcheck"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	valuespkg "helm.sh/helm/v3/pkg/cli/values"
)

type checkOptions struct {
	versionOverride    string
	preInstallOnly     bool
	dataPlaneOnly      bool
	wait               time.Duration
	namespace          string
	cniEnabled         bool
	output             string
	cliVersionOverride string
}

func newCheckOptions() *checkOptions {
	return &checkOptions{
		versionOverride:    "",
		preInstallOnly:     false,
		dataPlaneOnly:      false,
		wait:               300 * time.Second,
		namespace:          "",
		cniEnabled:         false,
		output:             tableOutput,
		cliVersionOverride: "",
	}
}

// nonConfigFlagSet specifies flags not allowed with `linkerd check config`
func (options *checkOptions) nonConfigFlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("non-config-check", pflag.ExitOnError)

	flags.BoolVar(&options.cniEnabled, "linkerd-cni-enabled", options.cniEnabled, "When running pre-installation checks (--pre), assume the linkerd-cni plugin is already installed, and a NET_ADMIN check is not needed")
	flags.StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace to use for --proxy checks (default: all namespaces)")
	flags.BoolVar(&options.preInstallOnly, "pre", options.preInstallOnly, "Only run pre-installation checks, to determine if the control plane can be installed")
	flags.BoolVar(&options.dataPlaneOnly, "proxy", options.dataPlaneOnly, "Only run data-plane checks, to determine if the data plane is healthy")

	return flags
}

// checkFlagSet specifies flags allowed with and without `config`
func (options *checkOptions) checkFlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("check", pflag.ExitOnError)

	flags.StringVar(&options.versionOverride, "expected-version", options.versionOverride, "Overrides the version used when checking if Linkerd is running the latest version (mostly for testing)")
	flags.StringVar(&options.cliVersionOverride, "cli-version-override", "", "Used to override the version of the cli (mostly for testing)")
	flags.StringVarP(&options.output, "output", "o", options.output, "Output format. One of: basic, json")
	flags.DurationVar(&options.wait, "wait", options.wait, "Maximum allowed time for all tests to pass")

	return flags
}

func (options *checkOptions) validate() error {
	if options.preInstallOnly && options.dataPlaneOnly {
		return errors.New("--pre and --proxy flags are mutually exclusive")
	}
	if !options.preInstallOnly && options.cniEnabled {
		return errors.New("--linkerd-cni-enabled can only be used with --pre")
	}
	if options.output != tableOutput && options.output != jsonOutput {
		return fmt.Errorf("Invalid output type '%s'. Supported output types are: %s, %s", options.output, jsonOutput, tableOutput)
	}
	return nil
}

// newCmdCheckConfig is a subcommand for `linkerd check config`
func newCmdCheckConfig(options *checkOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config [flags]",
		Args:  cobra.NoArgs,
		Short: "Check the Linkerd cluster-wide resources for potential problems",
		Long: `Check the Linkerd cluster-wide resources for potential problems.

The check command will perform a series of checks to validate that the Linkerd
cluster-wide resources are configured correctly. It is intended to validate that
"linkerd install config" succeeded. If the command encounters a failure it will
print additional information about the failure and exit with a non-zero exit
code.`,
		Example: `  # Check that the Linkerd cluster-wide resource are installed correctly
  linkerd check config`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return configureAndRunChecks(cmd, stdout, stderr, configStage, options)
		},
	}

	return cmd
}

func newCmdCheck() *cobra.Command {
	options := newCheckOptions()
	checkFlags := options.checkFlagSet()
	nonConfigFlags := options.nonConfigFlagSet()

	cmd := &cobra.Command{
		Use:   fmt.Sprintf("check [%s] [flags]", configStage),
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

  # Check that "linkerd install config" succeeded
  linkerd check config

  # Check that the Linkerd data plane proxies in the "app" namespace are up and running
  linkerd check --proxy --namespace app`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return configureAndRunChecks(cmd, stdout, stderr, "", options)
		},
	}

	cmd.PersistentFlags().AddFlagSet(checkFlags)
	cmd.Flags().AddFlagSet(nonConfigFlags)

	cmd.AddCommand(newCmdCheckConfig(options))

	return cmd
}

func configureAndRunChecks(cmd *cobra.Command, wout io.Writer, werr io.Writer, stage string, options *checkOptions) error {
	err := options.validate()
	if err != nil {
		return fmt.Errorf("Validation error when executing check command: %v", err)
	}

	if options.cliVersionOverride != "" {
		version.Version = options.cliVersionOverride
	}

	checks := []healthcheck.CategoryID{
		healthcheck.KubernetesAPIChecks,
		healthcheck.KubernetesVersionChecks,
		healthcheck.LinkerdVersionChecks,
	}

	var installManifest string
	if options.preInstallOnly {
		checks = append(checks, healthcheck.LinkerdPreInstallChecks)
		if options.cniEnabled {
			checks = append(checks, healthcheck.LinkerdCNIPluginChecks)
		} else {
			checks = append(checks, healthcheck.LinkerdPreInstallCapabilityChecks)
		}
		installManifest, err = renderInstallManifest(cmd.Context())
		if err != nil {
			return fmt.Errorf("Error rendering install manifest: %v", err)
		}
	} else {
		checks = append(checks, healthcheck.LinkerdConfigChecks)

		if stage != configStage {
			checks = append(checks, healthcheck.LinkerdControlPlaneExistenceChecks)
			checks = append(checks, healthcheck.LinkerdIdentity)
			checks = append(checks, healthcheck.LinkerdWebhooksAndAPISvcTLS)
			checks = append(checks, healthcheck.LinkerdControlPlaneProxyChecks)

			if options.dataPlaneOnly {
				checks = append(checks, healthcheck.LinkerdDataPlaneChecks)
				checks = append(checks, healthcheck.LinkerdIdentityDataPlane)
			} else {
				checks = append(checks, healthcheck.LinkerdControlPlaneVersionChecks)
			}
			checks = append(checks, healthcheck.LinkerdCNIPluginChecks)
			checks = append(checks, healthcheck.LinkerdHAChecks)

		}
	}

	hc := healthcheck.NewHealthChecker(checks, &healthcheck.Options{
		ControlPlaneNamespace: controlPlaneNamespace,
		CNINamespace:          cniNamespace,
		DataPlaneNamespace:    options.namespace,
		KubeConfig:            kubeconfigPath,
		KubeContext:           kubeContext,
		Impersonate:           impersonate,
		ImpersonateGroup:      impersonateGroup,
		APIAddr:               apiAddr,
		VersionOverride:       options.versionOverride,
		RetryDeadline:         time.Now().Add(options.wait),
		CNIEnabled:            options.cniEnabled,
		InstallManifest:       installManifest,
	})

	success := healthcheck.RunChecks(wout, werr, hc, options.output)

	extensionSuccess, err := runExtensionChecks(cmd, wout, werr, options)
	if err != nil {
		err = fmt.Errorf("failed to run extensions checks: %s", err)
		fmt.Fprintln(werr, err)
		os.Exit(1)
	}

	if !success || !extensionSuccess {
		os.Exit(1)
	}

	return nil
}

func runExtensionChecks(cmd *cobra.Command, wout io.Writer, werr io.Writer, opts *checkOptions) (bool, error) {
	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return false, err
	}

	namespaces, err := kubeAPI.GetAllNamespacesWithExtensionLabel(cmd.Context())
	if err != nil {
		return false, err
	}

	success := true
	// no extensions to check
	if len(namespaces) == 0 {
		return success, nil
	}

	if opts.output != healthcheck.JSONOutput {
		headerTxt := "Linkerd extensions checks"
		fmt.Fprintln(wout)
		fmt.Fprintln(wout, headerTxt)
		fmt.Fprintln(wout, strings.Repeat("=", len(headerTxt)))
	}

	spin := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	spin.Writer = wout

	for i, ns := range namespaces {
		if opts.output != healthcheck.JSONOutput && i < len(namespaces) {
			// add a new line to space out each check output
			fmt.Fprintln(wout)
		}
		extension := ns.Labels[k8s.LinkerdExtensionLabel]

		var path string
		args := append([]string{"check"}, getExtensionCheckFlags(cmd.Flags())...)
		var err error
		results := healthcheck.CheckResults{
			Results: []healthcheck.CheckResult{},
		}
		extensionCmd := fmt.Sprintf("linkerd-%s", extension)

		switch extension {
		case jaegerCmd.JaegerExtensionName:
			path = os.Args[0]
			args = append([]string{"jaeger"}, args...)
		case vizHealthCheck.VizExtensionName:
			path = os.Args[0]
			args = append([]string{"viz"}, args...)
		case mcCmd.MulticlusterExtensionName:
			path = os.Args[0]
			args = append([]string{"multicluster"}, args...)
		default:
			path, err = exec.LookPath(extensionCmd)
			results.Results = []healthcheck.CheckResult{
				{
					Category:    healthcheck.CategoryID(extensionCmd),
					Description: fmt.Sprintf("Linkerd extension command %s exists", extensionCmd),
					Err:         err,
					HintURL:     healthcheck.DefaultHintBaseURL + "extensions",
					Warning:     true,
				},
			}
		}
		if err == nil {
			if isatty.IsTerminal(os.Stdout.Fd()) {
				spin.Suffix = fmt.Sprintf(" Running %s extension check", extension)
				spin.Color("bold") // this calls spin.Restart()
			}
			plugin := exec.Command(path, args...)
			var stdout, stderr bytes.Buffer
			plugin.Stdout = &stdout
			plugin.Stderr = &stderr
			plugin.Run()
			extensionResults, err := healthcheck.ParseJSONCheckOutput(stdout.Bytes())
			spin.Stop()
			if err != nil {
				command := fmt.Sprintf("%s %s", path, strings.Join(args, " "))
				if len(stderr.String()) > 0 {
					err = errors.New(stderr.String())
				} else {
					err = fmt.Errorf("invalid extension check output from \"%s\" (JSON object expected):\n%s\n[%s]", command, stdout.String(), err)
				}
				results.Results = append(results.Results, healthcheck.CheckResult{
					Category:    healthcheck.CategoryID(extensionCmd),
					Description: fmt.Sprintf("Running: %s", command),
					Err:         err,
					HintURL:     healthcheck.DefaultHintBaseURL + "extensions",
				})
				success = false
			} else {
				results.Results = append(results.Results, extensionResults.Results...)
			}
		}
		extensionSuccess := healthcheck.RunChecks(wout, werr, results, opts.output)
		if !extensionSuccess {
			success = false
		}
	}
	return success, nil
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

func renderInstallManifest(ctx context.Context) (string, error) {
	values, err := charts.NewValues()
	if err != nil {
		return "", err
	}

	var b strings.Builder
	err = install(ctx, &b, values, []flag.Flag{}, "", valuespkg.Options{})
	if err != nil {
		return "", err
	}
	return b.String(), nil
}
