package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/pkg/version"

	"github.com/briandowns/spinner"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type checkOptions struct {
	versionOverride    string
	preInstallOnly     bool
	multicluster       bool
	dataPlaneOnly      bool
	wait               time.Duration
	namespace          string
	cniEnabled         bool
	output             string
	cliVersionOverride string
}

func newCheckOptions() *checkOptions {
	return &checkOptions{
		multicluster:       false,
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
	flags.BoolVar(&options.multicluster, "multicluster", options.multicluster, "Run multicluster checks")

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
			return configureAndRunChecks(stdout, stderr, configStage, options)
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
			return configureAndRunChecks(stdout, stderr, "", options)
		},
	}

	cmd.PersistentFlags().AddFlagSet(checkFlags)
	cmd.Flags().AddFlagSet(nonConfigFlags)

	cmd.AddCommand(newCmdCheckConfig(options))

	return cmd
}

func configureAndRunChecks(wout io.Writer, werr io.Writer, stage string, options *checkOptions) error {
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
		installManifest, err = renderInstallManifest()
		if err != nil {
			return fmt.Errorf("Error rendering install manifest: %v", err)
		}
	} else {
		checks = append(checks, healthcheck.LinkerdConfigChecks)

		if stage != configStage {
			checks = append(checks, healthcheck.LinkerdControlPlaneExistenceChecks)
			checks = append(checks, healthcheck.LinkerdAPIChecks)
			checks = append(checks, healthcheck.LinkerdIdentity)

			if options.dataPlaneOnly {
				checks = append(checks, healthcheck.LinkerdDataPlaneChecks)
				checks = append(checks, healthcheck.LinkerdIdentityDataPlane)
			} else {
				checks = append(checks, healthcheck.LinkerdControlPlaneVersionChecks)
			}
			checks = append(checks, healthcheck.LinkerdCNIPluginChecks)
			checks = append(checks, healthcheck.LinkerdHAChecks)
			checks = append(checks, healthcheck.LinkerdMulticlusterChecks)

			checks = append(checks, healthcheck.AddOnCategories...)
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

	success := runChecks(wout, werr, hc, options.output)

	if !success {
		os.Exit(1)
	}

	return nil
}

func runChecks(wout io.Writer, werr io.Writer, hc *healthcheck.HealthChecker, output string) bool {
	if output == jsonOutput {
		return runChecksJSON(wout, werr, hc)
	}
	return runChecksTable(wout, hc)
}

func runChecksTable(wout io.Writer, hc *healthcheck.HealthChecker) bool {
	var lastCategory healthcheck.CategoryID
	spin := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	spin.Writer = wout

	prettyPrintResults := func(result *healthcheck.CheckResult) {
		if lastCategory != result.Category {
			if lastCategory != "" {
				fmt.Fprintln(wout)
			}

			fmt.Fprintln(wout, result.Category)
			fmt.Fprintln(wout, strings.Repeat("-", len(result.Category)))

			lastCategory = result.Category
		}

		spin.Stop()
		if result.Retry {
			if isatty.IsTerminal(os.Stdout.Fd()) {
				spin.Suffix = fmt.Sprintf(" %s", result.Err)
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

		fmt.Fprintf(wout, "%s %s\n", status, result.Description)
		if result.Err != nil {
			fmt.Fprintf(wout, "    %s\n", result.Err)
			if result.HintAnchor != "" {
				fmt.Fprintf(wout, "    see %s%s for hints\n", healthcheck.HintBaseURL, result.HintAnchor)
			}
		}
	}

	success := hc.RunChecks(prettyPrintResults)
	// this empty line separates final results from the checks list in the output
	fmt.Fprintln(wout, "")

	if !success {
		fmt.Fprintf(wout, "Status check results are %s\n", failStatus)
	} else {
		fmt.Fprintf(wout, "Status check results are %s\n", okStatus)
	}

	return success
}

type checkOutput struct {
	Success    bool             `json:"success"`
	Categories []*checkCategory `json:"categories"`
}

type checkCategory struct {
	Name   string   `json:"categoryName"`
	Checks []*check `json:"checks"`
}

// check is a user-facing version of `healthcheck.CheckResult`, for output via
// `linkerd check -o json`.
type check struct {
	Description string      `json:"description"`
	Hint        string      `json:"hint,omitempty"`
	Error       string      `json:"error,omitempty"`
	Result      checkResult `json:"result"`
}

type checkResult string

const (
	checkSuccess checkResult = "success"
	checkWarn    checkResult = "warning"
	checkErr     checkResult = "error"
)

func runChecksJSON(wout io.Writer, werr io.Writer, hc *healthcheck.HealthChecker) bool {
	var categories []*checkCategory

	collectJSONOutput := func(result *healthcheck.CheckResult) {
		categoryName := string(result.Category)
		if categories == nil || categories[len(categories)-1].Name != categoryName {
			categories = append(categories, &checkCategory{
				Name:   categoryName,
				Checks: []*check{},
			})
		}

		if !result.Retry {
			currentCategory := categories[len(categories)-1]
			// ignore checks that are going to be retried, we want only final results
			status := checkSuccess
			if result.Err != nil {
				status = checkErr
				if result.Warning {
					status = checkWarn
				}
			}

			currentCheck := &check{
				Description: result.Description,
				Result:      status,
			}

			if result.Err != nil {
				currentCheck.Error = result.Err.Error()

				if result.HintAnchor != "" {
					currentCheck.Hint = fmt.Sprintf("%s%s", healthcheck.HintBaseURL, result.HintAnchor)
				}
			}
			currentCategory.Checks = append(currentCategory.Checks, currentCheck)
		}
	}

	result := hc.RunChecks(collectJSONOutput)

	outputJSON := checkOutput{
		Success:    result,
		Categories: categories,
	}

	resultJSON, err := json.MarshalIndent(outputJSON, "", "  ")
	if err == nil {
		fmt.Fprintf(wout, "%s\n", string(resultJSON))
	} else {
		fmt.Fprintf(werr, "JSON serialization of the check result failed with %s", err)
	}
	return result
}

func renderInstallManifest() (string, error) {
	options, err := newInstallOptionsWithDefaults()
	if err != nil {
		return "", err
	}
	values, _, err := options.validateAndBuild("", nil)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := render(&b, values); err != nil {
		return "", err
	}
	return b.String(), nil
}
