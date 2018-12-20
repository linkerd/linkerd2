package version

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
)

// Version is updated automatically as part of the build process
//
// DO NOT EDIT
var Version = undefinedVersion

const (
	undefinedVersion = "undefined"
	versionCheckURL  = "https://versioncheck.linkerd.io/version.json?version=%s&uuid=%s&source=%s"
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

func CheckClientVersion(expectedVersion string) error {
	if Version != expectedVersion {
		return versionMismatchError(expectedVersion, Version)
	}

	return nil
}

func CheckServerVersion(apiClient pb.ApiClient, expectedVersion string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rsp, err := apiClient.Version(ctx, &pb.Empty{})
	if err != nil {
		return err
	}

	if v := rsp.GetReleaseVersion(); v != expectedVersion {
		return versionMismatchError(expectedVersion, v)
	}

	return nil
}

func GetLatestVersion(uuid string, source string) (string, error) {
	url := fmt.Sprintf(versionCheckURL, Version, uuid, source)
	req, err := http.NewRequest("GET", url, nil)
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

	channel := parseChannel(Version)
	if channel == "" {
		return "", fmt.Errorf("Unsupported version format: %s", Version)
	}

	version, ok := versionRsp[channel]
	if !ok {
		return "", fmt.Errorf("Unsupported version channel: %s", channel)
	}

	return version, nil
}

func parseVersion(version string) string {
	if parts := strings.SplitN(version, "-", 2); len(parts) == 2 {
		return parts[1]
	}
	return version
}

func parseChannel(version string) string {
	if parts := strings.SplitN(version, "-", 2); len(parts) == 2 {
		return parts[0]
	}
	return ""
}

func versionMismatchError(expectedVersion, actualVersion string) error {
	channel := parseChannel(expectedVersion)
	expectedVersionStr := parseVersion(expectedVersion)
	actualVersionStr := parseVersion(actualVersion)

	if channel != "" {
		return fmt.Errorf("is running version %s but the latest %s version is %s",
			actualVersionStr, channel, expectedVersionStr)
	}

	return fmt.Errorf("is running version %s but the latest version is %s",
		actualVersionStr, expectedVersionStr)
}
