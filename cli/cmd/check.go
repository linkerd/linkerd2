package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/cli/flag"
	jaegerCmd "github.com/linkerd/linkerd2/jaeger/cmd"
	mcCmd "github.com/linkerd/linkerd2/multicluster/cmd"
	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	vizCmd "github.com/linkerd/linkerd2/viz/cmd"
	vizHealthCheck "github.com/linkerd/linkerd2/viz/pkg/healthcheck"
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
			return configureAndRunChecks(cmd.Context(), stdout, stderr, configStage, options)
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
			return configureAndRunChecks(cmd.Context(), stdout, stderr, "", options)
		},
	}

	cmd.PersistentFlags().AddFlagSet(checkFlags)
	cmd.Flags().AddFlagSet(nonConfigFlags)

	cmd.AddCommand(newCmdCheckConfig(options))

	return cmd
}

func configureAndRunChecks(ctx context.Context, wout io.Writer, werr io.Writer, stage string, options *checkOptions) error {
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
		installManifest, err = renderInstallManifest(ctx)
		if err != nil {
			return fmt.Errorf("Error rendering install manifest: %v", err)
		}
	} else {
		checks = append(checks, healthcheck.LinkerdConfigChecks)

		if stage != configStage {
			checks = append(checks, healthcheck.LinkerdControlPlaneExistenceChecks)
			checks = append(checks, healthcheck.LinkerdAPIChecks)
			checks = append(checks, healthcheck.LinkerdIdentity)
			checks = append(checks, healthcheck.LinkerdWebhooksAndAPISvcTLS)

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

	if !success {
		os.Exit(1)
	}

	err = runExtensionChecks(ctx, wout, werr, options)
	if err != nil {
		return fmt.Errorf("Error running extension checks: %v", err)
	}

	return nil
}

func runExtensionChecks(ctx context.Context, wout io.Writer, werr io.Writer, opts *checkOptions) error {
	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return err
	}

	namespaces, err := kubeAPI.GetAllNamespacesWithExtensionLabel(ctx)
	if err != nil {
		return err
	}

	// no extensions to check
	if len(namespaces) == 0 {
		return nil
	}

	headerTxt := "Linkerd extension checks"
	fmt.Fprintln(wout, "")
	fmt.Fprintln(wout, headerTxt)
	fmt.Fprintln(wout, strings.Repeat("=", len(headerTxt)))
	fmt.Fprintln(wout, "")

	noArgs := []string{}
	for i, ns := range namespaces {
		switch ns.Labels[k8s.LinkerdExtensionLabel] {
		case jaegerCmd.JaegerExtensionName:
			jaegerCheckCmd := jaegerCmd.NewCmdCheck()

			err = setSubCheckFlags(jaegerCheckCmd, map[string]string{
				"output": opts.output,
				"wait":   opts.wait.String(),
			})
			if err != nil {
				return err
			}

			err = jaegerCheckCmd.RunE(jaegerCheckCmd, noArgs)
		case vizHealthCheck.VizExtensionName:
			vizCheckCmd := vizCmd.NewCmdCheck()

			err = setSubCheckFlags(vizCheckCmd, map[string]string{
				"output":    opts.output,
				"wait":      opts.wait.String(),
				"proxy":     fmt.Sprintf("%t", opts.dataPlaneOnly),
				"namespace": opts.namespace,
			})
			if err != nil {
				return err
			}

			err = vizCheckCmd.RunE(vizCheckCmd, noArgs)
		case mcCmd.MulticlusterExtensionName:
			mcCheckCmd := mcCmd.NewCmdCheck()

			err = setSubCheckFlags(mcCheckCmd, map[string]string{
				"output": opts.output,
				"wait":   opts.wait.String(),
			})
			if err != nil {
				return err
			}

			err = mcCheckCmd.RunE(mcCheckCmd, noArgs)
		default:
			// Since we don't have checks for extensions we don't support
			// create a healthchecker that checks if the namespace exists
			categoryID := healthcheck.CategoryID(ns.Labels[k8s.LinkerdExtensionLabel])
			checkers := []healthcheck.Checker{}
			checkers = append(checkers,
				*healthcheck.NewChecker(fmt.Sprintf("%s extension Namespace exists", categoryID)).
					WithHintAnchor(fmt.Sprintf("%s-ns-exists", categoryID)).
					Fatal().
					WithCheck(func(ctx context.Context) error {
						_, err := kubeAPI.GetNamespaceWithExtensionLabel(ctx, string(categoryID))
						if err != nil {
							return err
						}
						return nil
					}))
			hc := healthcheck.NewHealthChecker([]healthcheck.CategoryID{categoryID},
				&healthcheck.Options{
					ControlPlaneNamespace: controlPlaneNamespace,
					CNINamespace:          cniNamespace,
					DataPlaneNamespace:    opts.namespace,
					KubeConfig:            kubeconfigPath,
					KubeContext:           kubeContext,
					Impersonate:           impersonate,
					ImpersonateGroup:      impersonateGroup,
					APIAddr:               apiAddr,
					VersionOverride:       opts.versionOverride,
					RetryDeadline:         time.Now().Add(opts.wait),
					CNIEnabled:            opts.cniEnabled,
				})
			hc.AppendCategories(*healthcheck.NewCategory(categoryID, checkers, true))
			healthcheck.RunChecks(wout, werr, hc, opts.output)
		}
		if err != nil {
			return err
		}

		if i+1 != len(namespaces) {
			// add a new line to space out each check output
			fmt.Fprintln(wout, "")
		}
	}
	return nil
}

func setSubCheckFlags(cmd *cobra.Command, flags map[string]string) error {
	for fName, fValue := range flags {
		err := cmd.Flags().Set(fName, fValue)
		if err != nil {
			return err
		}
	}
	return nil
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
