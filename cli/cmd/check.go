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
}

func newCheckOptions() *checkOptions {
	return &checkOptions{
		versionOverride: "",
	}
}

func newCmdCheck() *cobra.Command {
	options := newCheckOptions()

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check your Linkerd installation for potential problems.",
		Long: `Check your Linkerd installation for potential problems. The check command will perform various checks of your
local system, the Linkerd control plane, and connectivity between those. The process will exit with non-zero check if
problems were found.`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			configureAndRunChecks(options)
		},
	}

	cmd.Args = cobra.NoArgs
	cmd.PersistentFlags().StringVar(&options.versionOverride, "expected-version", options.versionOverride, "Overrides the version used when checking if Linkerd is running the latest version (mostly for testing)")

	return cmd
}

func configureAndRunChecks(options *checkOptions) {
	hc := healthcheck.NewHealthChecker()
	hc.AddKubernetesAPIChecks(kubeconfigPath)
	hc.AddLinkerdAPIChecks(apiAddr, controlPlaneNamespace)
	hc.AddLinkerdVersionChecks(options.versionOverride)

	success := runChecks(os.Stdout, hc)

	fmt.Println("")

	if !success {
		fmt.Printf("Status check results are %s\n", failStatus)
		os.Exit(2)
	}

	fmt.Printf("Status check results are %s\n", okStatus)
}

func runChecks(w io.Writer, hc *healthcheck.HealthChecker) bool {
	prettyPrintResults := func(category, description string, err error) {
		checkLabel := fmt.Sprintf("%s: %s", category, description)

		filler := ""
		lineBreak := "\n"
		for i := 0; i < lineWidth-len(checkLabel)-len(okStatus)-len(lineBreak); i++ {
			filler = filler + "."
		}

		if err != nil {
			fmt.Fprintf(w, "%s%s%s  -- %s%s", checkLabel, filler, failStatus, err.Error(), lineBreak)
			return
		}

		fmt.Fprintf(w, "%s%s%s%s", checkLabel, filler, okStatus, lineBreak)
	}

	return hc.RunChecks(prettyPrintResults)
}
