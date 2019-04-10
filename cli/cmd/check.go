package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/spf13/cobra"
)

type checkOptions struct {
	versionOverride string
	preInstallOnly  bool
	dataPlaneOnly   bool
	wait            time.Duration
	namespace       string
	cniEnabled      bool
	output          string
}

func newCheckOptions() *checkOptions {
	return &checkOptions{
		versionOverride: "",
		preInstallOnly:  false,
		dataPlaneOnly:   false,
		wait:            300 * time.Second,
		namespace:       "",
		cniEnabled:      false,
		output:          tableOutput,
	}
}

func newCmdCheck() *cobra.Command {
	options := newCheckOptions()

	cmd := &cobra.Command{
		Use:   "check",
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
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return configureAndRunChecks(stdout, stderr, options)
		},
	}

	cmd.Args = cobra.NoArgs
	cmd.PersistentFlags().StringVar(&options.versionOverride, "expected-version", options.versionOverride, "Overrides the version used when checking if Linkerd is running the latest version (mostly for testing)")
	cmd.PersistentFlags().BoolVar(&options.preInstallOnly, "pre", options.preInstallOnly, "Only run pre-installation checks, to determine if the control plane can be installed")
	cmd.PersistentFlags().BoolVar(&options.dataPlaneOnly, "proxy", options.dataPlaneOnly, "Only run data-plane checks, to determine if the data plane is healthy")
	cmd.PersistentFlags().DurationVar(&options.wait, "wait", options.wait, "Maximum allowed time for all tests to pass")
	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace to use for --proxy checks (default: all namespaces)")
	cmd.PersistentFlags().BoolVar(&options.cniEnabled, "linkerd-cni-enabled", options.cniEnabled, "When running pre-installation checks (--pre), assume the linkerd-cni plugin is already installed, and a NET_ADMIN check is not needed")
	cmd.PersistentFlags().StringVarP(&options.output, "output", "o", options.output, "Output format. One of: basic, json")

	return cmd
}

func configureAndRunChecks(wout io.Writer, werr io.Writer, options *checkOptions) error {
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
		checks = append(checks, healthcheck.LinkerdControlPlaneExistenceChecks)
		checks = append(checks, healthcheck.LinkerdAPIChecks)

		if options.dataPlaneOnly {
			checks = append(checks, healthcheck.LinkerdDataPlaneChecks)
		} else {
			checks = append(checks, healthcheck.LinkerdControlPlaneVersionChecks)
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

	success := runChecks(wout, werr, hc, options.output)

	if !success {
		os.Exit(2)
	}

	return nil
}

func (o *checkOptions) validate() error {
	if o.preInstallOnly && o.dataPlaneOnly {
		return errors.New("--pre and --proxy flags are mutually exclusive")
	}
	if o.output != tableOutput && o.output != jsonOutput {
		return fmt.Errorf("Invalid output type '%s'. Supported output types are: %s, %s", o.output, jsonOutput, tableOutput)
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
			spin.Suffix = fmt.Sprintf(" %s -- %s", result.Description, result.Err)
			spin.Color("bold")
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

type checkCategory struct {
	Name   string   `json:"categoryName"`
	Checks []*check `json:"checks"`
}

type check struct {
	Description string      `json:"description"`
	Hint        string      `json:"hint"`
	Result      checkResult `json:"result"`
}

type checkResult string

const (
	checkSuccess checkResult = "success"
	checkWarn    checkResult = "warning"
	checkErr     checkResult = "error"
)

func runChecksJSON(wout io.Writer, werr io.Writer, hc *healthcheck.HealthChecker) bool {
	var lastCategory healthcheck.CategoryID
	var outputJSON []*checkCategory
	var currentCategory *checkCategory

	collectJSONOutput := func(result *healthcheck.CheckResult) {
		if lastCategory != result.Category {
			currentCategory = &checkCategory{
				Name:   string(result.Category),
				Checks: []*check{},
			}
			outputJSON = append(outputJSON, currentCategory)

			lastCategory = result.Category
		}

		if !result.Retry {
			// ignore checks that are going to be retried, we want only final results
			status := checkSuccess
			if result.Err != nil {
				status = checkErr
				if result.Warning {
					status = checkWarn
				}
			}

			var hint string
			if result.Err != nil && result.HintAnchor != "" {
				hint = fmt.Sprintf("%s%s", healthcheck.HintBaseURL, result.HintAnchor)
			}

			currentCheck := &check{
				Description: result.Description,
				Result:      status,
				Hint:        hint,
			}
			currentCategory.Checks = append(currentCategory.Checks, currentCheck)
		}
	}

	result := hc.RunChecks(collectJSONOutput)
	resultJSON, err := json.MarshalIndent(outputJSON, "", "  ")
	if err == nil {
		fmt.Fprint(wout, string(resultJSON))
	} else {
		fmt.Fprintf(werr, "JSON serialization of the check result failed with %s", err)
	}
	return result
}
