package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type checkOptions struct {
	versionOverride string
	preInstallOnly  bool
	dataPlaneOnly   bool
	wait            time.Duration
	namespace       string
	cniEnabled      bool
}

func newCheckOptions() *checkOptions {
	return &checkOptions{
		versionOverride: "",
		preInstallOnly:  false,
		dataPlaneOnly:   false,
		wait:            300 * time.Second,
		namespace:       "",
		cniEnabled:      false,
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
	flags.DurationVar(&options.wait, "wait", options.wait, "Maximum allowed time for all tests to pass")

	return flags
}

func (options *checkOptions) validate() error {
	if options.preInstallOnly && options.dataPlaneOnly {
		return errors.New("--pre and --proxy flags are mutually exclusive")
	}
	return nil
}

func newCmdCheck() *cobra.Command {
	options := newCheckOptions()
	checkFlags := options.checkFlagSet()
	nonConfigFlags := options.nonConfigFlagSet()

	cmd := &cobra.Command{
		Use:       fmt.Sprintf("check [%s] [flags]", configStage),
		Args:      cobra.OnlyValidArgs,
		ValidArgs: []string{configStage},
		Short:     "Check the Linkerd installation for potential problems",
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
			stage, err := validateArgs(args, nonConfigFlags, nil)
			if err != nil {
				return err
			}

			return configureAndRunChecks(stdout, stage, options)
		},
	}

	cmd.PersistentFlags().AddFlagSet(checkFlags)
	cmd.PersistentFlags().AddFlagSet(nonConfigFlags)

	return cmd
}

func configureAndRunChecks(w io.Writer, stage string, options *checkOptions) error {
	err := options.validate()
	if err != nil {
		return fmt.Errorf("Validation error when executing check command: %v", err)
	}
	checks := []healthcheck.CategoryID{
		healthcheck.KubernetesAPIChecks,
		healthcheck.KubernetesVersionChecks,
		healthcheck.LinkerdVersionChecks,
	}

	if options.preInstallOnly {
		checks = append(checks, healthcheck.LinkerdPreInstallChecks)
		if !options.cniEnabled {
			checks = append(checks, healthcheck.LinkerdPreInstallCapabilityChecks)
		}
	} else {
		checks = append(checks, healthcheck.LinkerdConfigChecks)

		if stage != configStage {
			checks = append(checks, healthcheck.LinkerdControlPlaneExistenceChecks)
			checks = append(checks, healthcheck.LinkerdAPIChecks)

			if options.dataPlaneOnly {
				checks = append(checks, healthcheck.LinkerdDataPlaneChecks)
			} else {
				checks = append(checks, healthcheck.LinkerdControlPlaneVersionChecks)
			}
		}
	}

	hc := healthcheck.NewHealthChecker(checks, &healthcheck.Options{
		ControlPlaneNamespace: controlPlaneNamespace,
		DataPlaneNamespace:    options.namespace,
		KubeConfig:            kubeconfigPath,
		KubeContext:           kubeContext,
		APIAddr:               apiAddr,
		VersionOverride:       options.versionOverride,
		RetryDeadline:         time.Now().Add(options.wait),
	})

	success := runChecks(w, hc)

	// this empty line separates final results from the checks list in the output
	fmt.Fprintln(w, "")

	if !success {
		fmt.Fprintf(w, "Status check results are %s\n", failStatus)
		os.Exit(2)
	}

	fmt.Fprintf(w, "Status check results are %s\n", okStatus)

	return nil
}

func runChecks(w io.Writer, hc *healthcheck.HealthChecker) bool {
	var lastCategory healthcheck.CategoryID
	spin := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	spin.Writer = w

	prettyPrintResults := func(result *healthcheck.CheckResult) {
		if lastCategory != result.Category {
			if lastCategory != "" {
				fmt.Fprintln(w)
			}

			fmt.Fprintln(w, result.Category)
			fmt.Fprintln(w, strings.Repeat("-", len(result.Category)))

			lastCategory = result.Category
		}

		spin.Stop()
		if result.Retry {
			if isatty.IsTerminal(os.Stdout.Fd()) {
				spin.Suffix = fmt.Sprintf(" %s -- %s", result.Description, result.Err)
				spin.Color("bold") // this calls spin.Restart()
			}
			return
		}

		status := okStatus
		if result.Err != nil {
			status = failStatus
			if result.Warning {
				status = warnStatus
			}
		}

		fmt.Fprintf(w, "%s %s\n", status, result.Description)
		if result.Err != nil {
			fmt.Fprintf(w, "    %s\n", result.Err)
			if result.HintAnchor != "" {
				fmt.Fprintf(w, "    see %s%s for hints\n", healthcheck.HintBaseURL, result.HintAnchor)
			}
		}
	}

	return hc.RunChecks(prettyPrintResults)
}
