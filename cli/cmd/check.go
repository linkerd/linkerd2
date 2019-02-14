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
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
)

type checkOptions struct {
	versionOverride     string
	preInstallOnly      bool
	targetProxyResource string
	dataPlaneOnly       bool
	wait                time.Duration
	namespace           string
	singleNamespace     bool
}

func newCheckOptions() *checkOptions {
	return &checkOptions{
		versionOverride:     "",
		preInstallOnly:      false,
		targetProxyResource: "",
		dataPlaneOnly:       false,
		wait:                300 * time.Second,
		namespace:           "",
		singleNamespace:     false,
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
			return configureAndRunChecks(cmd, stdout, options)
		},
	}

	cmd.Args = cobra.NoArgs
	cmd.PersistentFlags().StringVar(&options.versionOverride, "expected-version", options.versionOverride, "Overrides the version used when checking if Linkerd is running the latest version (mostly for testing)")
	cmd.PersistentFlags().BoolVar(&options.preInstallOnly, "pre", options.preInstallOnly, "Only run pre-installation checks, to determine if the control plane can be installed")
	cmd.PersistentFlags().StringVar(&options.targetProxyResource, "proxy", options.targetProxyResource, "Only run data-plane checks, to determine if the data plane is healthy")
	cmd.PersistentFlags().DurationVar(&options.wait, "wait", options.wait, "Maximum allowed time for all tests to pass")
	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace to use for --proxy checks (default: all namespaces)")
	cmd.PersistentFlags().BoolVar(&options.singleNamespace, "single-namespace", options.singleNamespace, "When running pre-installation checks (--pre), only check the permissions required to operate the control plane in a single namespace")

	return cmd
}

func configureAndRunChecks(cmd *cobra.Command, w io.Writer, options *checkOptions) error {
	if cmd.Flags().Changed("proxy") {
		options.dataPlaneOnly = true
	}

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
		if options.singleNamespace {
			checks = append(checks, healthcheck.LinkerdPreInstallSingleNamespaceChecks)
		} else {
			checks = append(checks, healthcheck.LinkerdPreInstallClusterChecks)
		}
		checks = append(checks, healthcheck.LinkerdPreInstallChecks)
	} else {
		checks = append(checks, healthcheck.LinkerdControlPlaneExistenceChecks)
		checks = append(checks, healthcheck.LinkerdAPIChecks)

		if !options.singleNamespace {
			checks = append(checks, healthcheck.LinkerdServiceProfileChecks)
		}

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
		TargetProxyResource:   options.targetProxyResource,
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

func (o *checkOptions) validate() error {
	if o.preInstallOnly && o.dataPlaneOnly {
		return errors.New("--pre and --proxy flags are mutually exclusive")
	}
	if o.targetProxyResource != "" {
		ownerParts := strings.Split(o.targetProxyResource, "/")
		if len(ownerParts) != 2 {
			return fmt.Errorf("Invalid resource name '%s'. Expecting target resource in the following format: 'type/name'. E.g. deployment/web", o.targetProxyResource)
		}
		_, err := k8s.CanonicalResourceNameFromFriendlyName(ownerParts[0])
		if err != nil {
			return fmt.Errorf("Invalid resource name '%s'. Expecting target resource in the following format: 'type/name'. E.g. deployment/web. Error: %s", o.targetProxyResource, err)
		}
	}

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
