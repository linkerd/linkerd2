package version

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	healthcheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/healthcheck"
	log "github.com/sirupsen/logrus"
)

// DO NOT EDIT
// This var is updated automatically as part of the build process
var Version = "undefined"

const (
	VersionSubsystemName         = "conduit-version"
	CliCheckDescription          = "cli is up-to-date"
	ControlPlaneCheckDescription = "control plane is up-to-date"
)

func VersionFlag() *bool {
	return flag.Bool("version", false, "print version and exit")
}

func MaybePrintVersionAndExit(printVersion bool) {
	if printVersion {
		fmt.Println(Version)
		os.Exit(0)
	}
	log.Infof("running conduit version %s", Version)
}

type versionStatusChecker struct {
	version         string
	versionCheckURL string
	client          pb.ApiClient
}

func (v versionStatusChecker) SelfCheck() []*healthcheckPb.CheckResult {
	cliVersion := v.version
	cliIsUpToDate := &healthcheckPb.CheckResult{
		Status:           healthcheckPb.CheckStatus_OK,
		SubsystemName:    VersionSubsystemName,
		CheckDescription: CliCheckDescription,
	}

	latestVersion, err := getLatestVersion(v.versionCheckURL)
	if err != nil {
		cliIsUpToDate.Status = healthcheckPb.CheckStatus_ERROR
		cliIsUpToDate.FriendlyMessageToUser = err.Error()
		return []*healthcheckPb.CheckResult{cliIsUpToDate}
	}
	if cliVersion != latestVersion {
		cliIsUpToDate.Status = healthcheckPb.CheckStatus_FAIL
		cliIsUpToDate.FriendlyMessageToUser = fmt.Sprintf("is running version %s but the latest version is %s", cliVersion, latestVersion)
	}

	controlPlaneIsUpToDate := &healthcheckPb.CheckResult{
		Status:           healthcheckPb.CheckStatus_OK,
		SubsystemName:    VersionSubsystemName,
		CheckDescription: ControlPlaneCheckDescription,
	}

	controlPlaneVersion, err := getServerVersion(v.client)
	if err != nil {
		controlPlaneIsUpToDate.Status = healthcheckPb.CheckStatus_ERROR
		controlPlaneIsUpToDate.FriendlyMessageToUser = err.Error()
		return []*healthcheckPb.CheckResult{controlPlaneIsUpToDate}
	}
	if controlPlaneVersion != latestVersion {
		controlPlaneIsUpToDate.Status = healthcheckPb.CheckStatus_FAIL
		controlPlaneIsUpToDate.FriendlyMessageToUser = fmt.Sprintf("is running version %s but the latest version is %s", controlPlaneVersion, latestVersion)
	}

	checks := []*healthcheckPb.CheckResult{cliIsUpToDate}
	checks = append(checks, controlPlaneIsUpToDate)
	return checks
}

func getServerVersion(client pb.ApiClient) (string, error) {
	resp, err := client.Version(context.Background(), &pb.Empty{})
	if err != nil {
		return "", err
	}

	return resp.GetReleaseVersion(), nil
}

func getLatestVersion(versionCheckURL string) (string, error) {
	resp, err := http.Get(versionCheckURL)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("got %d error from %s", resp.StatusCode, versionCheckURL)
	}

	defer resp.Body.Close()
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var l map[string]string
	err = json.Unmarshal(bodyBytes, &l)
	if err != nil {
		return "", err
	}

	return l["version"], nil
}

func NewVersionStatusChecker(versionCheckURL string, client pb.ApiClient) healthcheck.StatusChecker {
	return versionStatusChecker{
		version:         Version,
		versionCheckURL: versionCheckURL,
		client:          client,
	}
}
