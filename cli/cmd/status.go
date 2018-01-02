package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/runconduit/conduit/cli/healthcheck"

	"github.com/runconduit/conduit/cli/shell"
	"github.com/runconduit/conduit/pkg/k8s"
	"github.com/spf13/cobra"
)

const lineWidth = 52

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check your Conduit installation for potential problems.",
	Long: `Check your Conduit installation for potential problems. The status command will perform various checks of your
local system, the Conduit control plane, and connectivity between those. The process will exit with non-zero status if
problems were found.`,
	Args: cobra.NoArgs,
	Run: exitSilentlyOnError(func(cmd *cobra.Command, args []string) error {

		kubectl, err := k8s.MakeKubectl(shell.NewUnixShell())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
			return statusCheckResultWasError(os.Stdout)
		}

		return checkStatus(os.Stdout, kubectl)
	}),
}

func checkStatus(w io.Writer, kubectl k8s.Kubectl) error {
	prettyPrintResults := func(result healthcheck.CheckResult) {
		checkLabel := fmt.Sprintf("%s: %s", result.SubsystemName, result.CheckDescription)

		filler := ""
		for i := 0; i < lineWidth-len(checkLabel); i++ {
			filler = filler + "."
		}

		switch result.Status {
		case healthcheck.CheckOk:
			fmt.Fprintf(w, "%s%s[ok]\n", checkLabel, filler)
		case healthcheck.CheckFailed:
			fmt.Fprintf(w, "%s%s[FAIL]  -- %s\n", checkLabel, filler, result.NextSteps)
		case healthcheck.CheckError:
			fmt.Fprintf(w, "%s%s[ERROR] -- %s\n", checkLabel, filler, result.NextSteps)
		}
	}

	checker := healthcheck.MakeHealthChecker()
	checker.Add(kubectl)
	check := checker.PerformCheck(prettyPrintResults)

	fmt.Fprintln(w, "")

	var errBasedOnOverallStatus error
	switch check.OverallStatus {
	case healthcheck.CheckOk:
		errBasedOnOverallStatus = statusCheckResultWasOk(w)
	case healthcheck.CheckFailed:
		errBasedOnOverallStatus = statusCheckResultWasFail(w)
	case healthcheck.CheckError:
		errBasedOnOverallStatus = statusCheckResultWasError(w)
	}

	return errBasedOnOverallStatus
}

func statusCheckResultWasOk(w io.Writer) error {
	fmt.Fprintln(w, "Status check results are [ok]")
	return nil
}

func statusCheckResultWasFail(w io.Writer) error {
	fmt.Fprintln(w, "Status check results are [FAIL]")
	return errors.New("failed status check")
}

func statusCheckResultWasError(w io.Writer) error {
	fmt.Fprintln(w, "Status check results are [ERROR]")
	return errors.New("error during status check")
}

func init() {
	RootCmd.AddCommand(statusCmd)
	addControlPlaneNetworkingArgs(statusCmd)
}
