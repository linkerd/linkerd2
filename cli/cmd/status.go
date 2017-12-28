package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/runconduit/conduit/cli/healthcheck"

	"github.com/runconduit/conduit/cli/k8s"
	"github.com/runconduit/conduit/cli/shell"
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

		kubectl, err := k8s.MakeKubectl(shell.MakeUnixShell())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
			return statusCheckResultWasError(os.Stdout, err)
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

	var errBasedOnOverallStatus error
	switch check.OverallStatus {
	case healthcheck.CheckOk:
		errBasedOnOverallStatus = statusCheckResultWasOk(w, errBasedOnOverallStatus)
	case healthcheck.CheckFailed:
		errBasedOnOverallStatus = statusCheckResultWasFail(w, errBasedOnOverallStatus)
	case healthcheck.CheckError:
		errBasedOnOverallStatus = statusCheckResultWasError(w, errBasedOnOverallStatus)
	}

	return errBasedOnOverallStatus
}

func statusCheckResultWasOk(w io.Writer, errBasedOnOverallStatus error) error {
	fmt.Fprintln(w, "Status check results are [ok]")
	errBasedOnOverallStatus = nil
	return errBasedOnOverallStatus
}

func statusCheckResultWasFail(w io.Writer, errBasedOnOverallStatus error) error {
	fmt.Fprintln(w, "Status check results are [FAIL]")
	errBasedOnOverallStatus = errors.New("Failed status check")
	return errBasedOnOverallStatus
}

func statusCheckResultWasError(w io.Writer, errBasedOnOverallStatus error) error {
	fmt.Fprintln(w, "Status check results are [ERROR]")
	errBasedOnOverallStatus = errors.New("Error during status check")
	return errBasedOnOverallStatus
}

func init() {
	RootCmd.AddCommand(statusCmd)
	addControlPlaneNetworkingArgs(statusCmd)
}
