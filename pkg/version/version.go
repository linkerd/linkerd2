package version

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
)

// DO NOT EDIT
// This var is updated automatically as part of the build process
var Version = undefinedVersion

const (
	undefinedVersion = "undefined"
	versionCheckURL  = "https://versioncheck.linkerd.io/version.json"
)

func init() {
	// Use `$LINKERD_CONTAINER_VERSION_OVERRIDE` as the version only if the
	// version wasn't set at link time to minimize the chance of using it
	// unintentionally. This mechanism allows the version to be bound at
	// container build time instead of at executable link time to improve
	// incremental rebuild efficiency.
	if Version == undefinedVersion {
		override := os.Getenv("LINKERD_CONTAINER_VERSION_OVERRIDE")
		if override != "" {
			Version = override
		}
	}
}

func CheckClientVersion(latestVersion string) error {
	if Version != latestVersion {
		return fmt.Errorf("is running version %s but the latest version is %s",
			Version, latestVersion)
	}

	return nil
}

func CheckServerVersion(apiClient pb.ApiClient, latestVersion string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rsp, err := apiClient.Version(ctx, &pb.Empty{})
	if err != nil {
		return err
	}

	if rsp.GetReleaseVersion() != latestVersion {
		return fmt.Errorf("is running version %s but the latest version is %s",
			rsp.GetReleaseVersion(), latestVersion)
	}

	return nil
}

func GetLatestVersion() (string, error) {
	req, err := http.NewRequest("GET", versionCheckURL, nil)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rsp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return "", err
	}
	defer rsp.Body.Close()

	if rsp.StatusCode != 200 {
		return "", fmt.Errorf("Unexpected versioncheck response: %s", rsp.Status)
	}

	bytes, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		return "", err
	}

	var versionRsp map[string]string
	err = json.Unmarshal(bytes, &versionRsp)
	if err != nil {
		return "", err
	}

	return versionRsp["version"], nil
}
