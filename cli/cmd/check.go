package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/spf13/cobra"
)

const (
	lineWidth  = 80
	okStatus   = "[ok]"
	failStatus = "[FAIL]"
)

type checkOptions struct {
	versionOverride string
	preInstallOnly  bool
}

func newCheckOptions() *checkOptions {
	return &checkOptions{
		versionOverride: "",
		preInstallOnly:  false,
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
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			configureAndRunChecks(options)
		},
	}

	cmd.Args = cobra.NoArgs
	cmd.PersistentFlags().StringVar(&options.versionOverride, "expected-version", options.versionOverride, "Overrides the version used when checking if Linkerd is running the latest version (mostly for testing)")
	cmd.PersistentFlags().BoolVar(&options.preInstallOnly, "pre", options.preInstallOnly, "Only run pre-installation checks, to determine if the control plane can be installed")

	return cmd
}

func configureAndRunChecks(options *checkOptions) {
	checks := []healthcheck.Checks{healthcheck.KubernetesAPIChecks}
	if options.preInstallOnly {
		checks = append(checks, healthcheck.LinkerdPreInstallChecks)
	} else {
		checks = append(checks, healthcheck.LinkerdAPIChecks)
	}
	checks = append(checks, healthcheck.LinkerdVersionChecks)

	hc := healthcheck.NewHealthChecker(checks, &healthcheck.HealthCheckOptions{
		Namespace:                    controlPlaneNamespace,
		KubeConfig:                   kubeconfigPath,
		APIAddr:                      apiAddr,
		VersionOverride:              options.versionOverride,
		ShouldCheckKubeVersion:       true,
		ShouldCheckControllerVersion: !options.preInstallOnly,
	})

	success := runChecks(os.Stdout, hc)

	fmt.Println("")

	if !success {
		fmt.Printf("Status check results are %s\n", failStatus)
		os.Exit(2)
	}

	fmt.Printf("Status check results are %s\n", okStatus)
}

func runChecks(w io.Writer, hc *healthcheck.HealthChecker) bool {
	prettyPrintResults := func(result *healthcheck.CheckResult) {
		checkLabel := fmt.Sprintf("%s: %s", result.Category, result.Description)

		filler := ""
		lineBreak := "\n"
		for i := 0; i < lineWidth-len(checkLabel)-len(okStatus)-len(lineBreak); i++ {
			filler = filler + "."
		}

		if result.Err != nil {
			fmt.Fprintf(w, "%s%s%s -- %s%s", checkLabel, filler, failStatus, result.Err, lineBreak)
			return
		}

		fmt.Fprintf(w, "%s%s%s%s", checkLabel, filler, okStatus, lineBreak)
	}

	return hc.RunChecks(prettyPrintResults)
}
