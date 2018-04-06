package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/runconduit/conduit/controller/api/public"
	healthcheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/healthcheck"
	"github.com/runconduit/conduit/pkg/k8s"
	"github.com/runconduit/conduit/pkg/version"
	"github.com/spf13/cobra"
)

const (
	lineWidth       = 80
	okStatus        = "[ok]"
	failStatus      = "[FAIL]"
	errorStatus     = "[ERROR]"
	versionCheckURL = "https://versioncheck.conduit.io/version.json"
)

var versionOverride string

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check your Conduit installation for potential problems.",
	Long: `Check your Conduit installation for potential problems. The check command will perform various checks of your
local system, the Conduit control plane, and connectivity between those. The process will exit with non-zero check if
problems were found.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {

		kubeApi, err := k8s.NewAPI(kubeconfigPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error with Kubernetes API: %s\n", err.Error())
			statusCheckResultWasError(os.Stdout)
			os.Exit(2)
		}

		var conduitApi pb.ApiClient
		if apiAddr != "" {
			conduitApi, err = public.NewInternalClient(apiAddr)
		} else {
			conduitApi, err = public.NewExternalClient(controlPlaneNamespace, kubeApi)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error with Conduit API: %s\n", err.Error())
			statusCheckResultWasError(os.Stdout)
			os.Exit(2)
		}

		grpcStatusChecker := healthcheck.NewGrpcStatusChecker(public.ConduitApiSubsystemName, conduitApi)
		versionStatusChecker := version.NewVersionStatusChecker(versionCheckURL, versionOverride, conduitApi)

		err = checkStatus(os.Stdout, kubeApi, grpcStatusChecker, versionStatusChecker)
		if err != nil {
			os.Exit(2)
		}
	},
}

func checkStatus(w io.Writer, checkers ...healthcheck.StatusChecker) error {
	prettyPrintResults := func(result *healthcheckPb.CheckResult) {
		checkLabel := fmt.Sprintf("%s: %s", result.SubsystemName, result.CheckDescription)

		filler := ""
		lineBreak := "\n"
		for i := 0; i < lineWidth-len(checkLabel)-len(okStatus)-len(lineBreak); i++ {
			filler = filler + "."
		}

		switch result.Status {
		case healthcheckPb.CheckStatus_OK:
			fmt.Fprintf(w, "%s%s%s%s", checkLabel, filler, okStatus, lineBreak)
		case healthcheckPb.CheckStatus_FAIL:
			fmt.Fprintf(w, "%s%s%s  -- %s%s", checkLabel, filler, failStatus, result.FriendlyMessageToUser, lineBreak)
		case healthcheckPb.CheckStatus_ERROR:
			fmt.Fprintf(w, "%s%s%s -- %s%s", checkLabel, filler, errorStatus, result.FriendlyMessageToUser, lineBreak)
		}
	}

	checker := healthcheck.MakeHealthChecker()
	for _, c := range checkers {
		checker.Add(c)
	}

	checkStatus := checker.PerformCheck(prettyPrintResults)

	fmt.Fprintln(w, "")

	var err error
	switch checkStatus {
	case healthcheckPb.CheckStatus_OK:
		err = statusCheckResultWasOk(w)
	case healthcheckPb.CheckStatus_FAIL:
		err = statusCheckResultWasFail(w)
	case healthcheckPb.CheckStatus_ERROR:
		err = statusCheckResultWasError(w)
	}

	return err
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
	RootCmd.AddCommand(checkCmd)
	checkCmd.Args = cobra.NoArgs
	checkCmd.PersistentFlags().StringVar(&versionOverride, "expected-version", "", "Overrides the version used when checking if Conduit is running the latest version (mostly for testing)")
}
