package k8s

import (
	"fmt"
	"os/exec"
	"regexp"
)

// minKubectlVersion is effectively minAPIVersion.
// It's unlikely that the minimum supported kubectl version
// will be different from that of the k8s API.
var minKubectlVersion = minAPIVersion

// CheckKubectlVersion validates whether the installed kubectl version is
// running a minimum kubectl version.
func CheckKubectlVersion() error {
	cmd := exec.Command("kubectl", "version", "--client", "--short")
	bytes, err := cmd.Output()
	if err != nil {
		return err
	}

	clientVersion := fmt.Sprintf("%s\n", bytes)
	kubectlVersion, err := parseKubectlShortVersion(clientVersion)
	if err != nil {
		return err
	}

	if !isCompatibleVersion(minKubectlVersion, kubectlVersion) {
		return fmt.Errorf("kubectl is on version [%d.%d.%d], but version [%d.%d.%d] or more recent is required",
			kubectlVersion[0], kubectlVersion[1], kubectlVersion[2],
			minKubectlVersion[0], minKubectlVersion[1], minKubectlVersion[2])
	}

	return nil
}

var semVer = regexp.MustCompile("(v[0-9]+.[0-9]+.[0-9]+)")

func parseKubectlShortVersion(version string) ([3]int, error) {
	versionString := semVer.FindString(version)
	return getK8sVersion(versionString)
}
