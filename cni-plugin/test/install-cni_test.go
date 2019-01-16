// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package test

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const (
	hostCniNetDir    = "/host/etc/cni/net.d"
	cniNetSubDir     = "/data/pre/"
	k8sSvcAcctSubDir = "/data/k8s_svcacct/"

	cniConfName          = "CNI_CONF_NAME"
	cniNetworkConfigName = "CNI_NETWORK_CONFIG"
)

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func setEnv(key, value string, t *testing.T) {
	err := os.Setenv(key, value)
	if err != nil {
		t.Fatalf("Couldn't set environment variable, err: %v", err)
	}
}

func mktemp(dir, prefix string, t *testing.T) string {
	tempDir, err := ioutil.TempDir(dir, prefix)
	if err != nil {
		t.Fatalf("Couldn't get current working directory, err: %v", err)
	}
	t.Logf("Created temporary dir: %v", tempDir)
	return tempDir
}

func pwd(t *testing.T) string {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Couldn't get current working directory, err: %v", err)
	}
	return wd + "/"
}

func ls(dir string, t *testing.T) []string {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		t.Fatalf("Failed to list files, err: %v", err)
	}
	fileNames := make([]string, len(files))
	for i, f := range files {
		fileNames[i] = f.Name()
	}
	return fileNames
}

func cp(src, dest string, t *testing.T) {
	data, err := ioutil.ReadFile(src)
	if err != nil {
		t.Fatalf("Failed to read file %v, err: %v", src, err)
	}
	if err = ioutil.WriteFile(dest, data, 0644); err != nil {
		t.Fatalf("Failed to write file %v, err: %v", dest, err)
	}
}

func rm(dir string, t *testing.T) {
	err := os.RemoveAll(dir)
	if err != nil {
		t.Fatalf("Failed to remove dir %v, err: %v", dir, err)
	}
}

// populateTempDirs populates temporary test directories with golden files
func populateTempDirs(wd string, tempCNINetDir string, preConfFile string, t *testing.T) {
	t.Logf("Pre-populating working dirs")
	t.Logf("Copying %v into temp config dir %v", preConfFile, tempCNINetDir)
	cp(wd+cniNetSubDir+preConfFile, tempCNINetDir+"/"+preConfFile, t)
}

// populateK8sCreds populates temporary k8s directories with k8s credentials like service account token
func populateK8sCreds(wd string, tempK8sSvcAcctDir string, t *testing.T) {
	for _, f := range ls(wd+k8sSvcAcctSubDir, t) {
		t.Logf("Copying %v into temp k8s serviceaccount dir %v", f, tempK8sSvcAcctDir)
		cp(wd+k8sSvcAcctSubDir+f, tempK8sSvcAcctDir+"/"+f, t)
	}
	t.Logf("Finished pre-populating working dirs")
}

// startDocker starts a test Docker container and runs the install-cni.sh script.
func startDocker(testNum int, wd string, tempCNINetDir string, tempCNIBinDir string, tempK8sSvcAcctDir string, t *testing.T) string {
	// The following is in place to default to a sane development environment that mirrors how bin/fast-build
	// does it. To change to a different docker image, set the HUB and TAG environment variables before running the tests.
	// gitShaHead, _ := exec.Command("git", "rev-parse", "--short=8", "HEAD").Output()
	// user, _ := user.Current()
	// tag := "dev-" + strings.Trim(string(gitShaHead), "\n") + "-" + user.Username
	dockerImage := env("HUB", "gcr.io/linkerd-io") + "/cni-plugin:" + env("TAG", "dev-bd117b06-x27s")
	errFileName := wd + "/docker_run_stderr"

	// Build arguments list by picking whatever is necessary from the environment.
	args := []string{"run", "-d",
		"--name", "test-linkerd-cni-install",
		"-v", tempCNINetDir + ":" + hostCniNetDir,
		"-v", tempCNIBinDir + ":/host/opt/cni/bin",
		"-v", tempK8sSvcAcctDir + ":/var/run/secrets/kubernetes.io/serviceaccount",
		"--env-file", wd + "/data/env_vars.sh",
		"-e", cniNetworkConfigName,
		"-e", "SLEEP=true",
	}
	if _, ok := os.LookupEnv(cniConfName); ok {
		args = append(args, "-e", cniConfName)
	}
	args = append(args, dockerImage)
	args = append(args, "/install-cni.sh")

	// Create a temporary log file to write docker command error log.
	errFile, err := os.Create(errFileName)
	if err != nil {
		t.Fatalf("Couldn't create docker stderr file, err: %v", err)
	}
	defer func() {
		errClose := errFile.Close()
		if errClose != nil {
			t.Fatalf("Couldn't create docker stderr file, err: %v", errClose)
		}
	}()

	// Run the docker command and write errors to a temporary file.
	cmd := exec.Command("docker", args...)
	cmd.Stderr = errFile

	containerID, err := cmd.Output()
	if err != nil {
		t.Fatalf("Test %v ERROR: failed to start docker container '%v', see %v", testNum, dockerImage, errFileName)
	}
	t.Logf("Container ID: %s", containerID)
	return strings.Trim(string(containerID), "\n")
}

// docker runs the given docker command on the given container ID.
func docker(cmd, containerID string, t *testing.T) {
	out, err := exec.Command("docker", cmd, containerID).CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to execute 'docker %s %s', err: %v", cmd, containerID, err)
	}
	t.Logf("docker %s %s - out: %s", cmd, containerID, out)
}

// compareConfResult does a string compare of 2 test files.
func compareConfResult(testWorkRootDir string, tempCNINetDir string, result string, expected string, t *testing.T) {
	tempResult := tempCNINetDir + "/" + result
	resultFile, err := ioutil.ReadFile(tempResult)
	if err != nil {
		t.Fatalf("Failed to read file %v, err: %v", tempResult, err)
	}

	expectedFile, err := ioutil.ReadFile(expected)
	if err != nil {
		t.Fatalf("Failed to read file %v, err: %v", expected, err)
	}

	if bytes.Equal(resultFile, expectedFile) {
		t.Logf("PASS: result matches expected: %v v. %v", tempResult, expected)
	} else {
		tempFail := mktemp(testWorkRootDir, result+".fail.XXXX", t)
		t.Errorf("FAIL: result doesn't match expected: %v v. %v", tempResult, expected)
		cp(tempResult, tempFail+"/"+result, t)
		t.Fatalf("Check %v for diff contents", tempFail)
	}
}

// checkBinDir verifies the presence/absence of test files.
func checkBinDir(t *testing.T, tempCNIBinDir string, op string, files ...string) {
	for _, f := range files {
		if _, err := os.Stat(tempCNIBinDir + "/" + f); !os.IsNotExist(err) {
			if op == "add" {
				t.Logf("PASS: File %v was added to %v", f, tempCNIBinDir)
			} else if op == "del" {
				t.Fatalf("FAIL: File %v was not removed from %v", f, tempCNIBinDir)
			}
		} else {
			if op == "add" {
				t.Fatalf("FAIL: File %v was not added to %v", f, tempCNIBinDir)
			} else if op == "del" {
				t.Logf("PASS: File %v was removed from %v", f, tempCNIBinDir)
			}
		}
	}
}

// doTest sets up necessary environment variables, runs the Docker installation
// container and verifies output file correctness.
func doTest(testNum int, wd string, initialNetConfFile string, finalNetConfFile string, expectNetConfFile string, expectedPostCleanNetConfFile string, tempCNINetDir string, tempCNIBinDir string, tempK8sSvcAcctDir string, testWorkRootDir string, t *testing.T) {
	t.Logf("Test %v: prior cni-conf='%v', expected result='%v'", testNum, initialNetConfFile, finalNetConfFile)

	if initialNetConfFile != "NONE" {
		setEnv(cniConfName, initialNetConfFile, t)
	}
	defaultData, err := ioutil.ReadFile(wd + "../deployment/linkerd-cni.conf.default")
	if err != nil {
		t.Fatalf("Failed to read file %v, err: %v", wd+"../deployment/linkerd-cni.conf.default", err)
	}
	setEnv(cniNetworkConfigName, string(defaultData), t)

	containerID := startDocker(testNum, wd, tempCNINetDir, tempCNIBinDir, tempK8sSvcAcctDir, t)
	time.Sleep(5 * time.Second)

	compareConfResult(testWorkRootDir, tempCNINetDir, finalNetConfFile, expectNetConfFile, t)
	checkBinDir(t, tempCNIBinDir, "add", "linkerd-cni")

	docker("stop", containerID, t)
	time.Sleep(5 * time.Second)

	t.Logf("Test %v: Check the cleanup worked", testNum)
	checkBinDir(t, tempCNIBinDir, "del", "linkerd-cni")
	if len(expectedPostCleanNetConfFile) > 0 {
		compareConfResult(testWorkRootDir, tempCNINetDir, finalNetConfFile, expectedPostCleanNetConfFile, t)
	} else {
		files := ls(tempCNINetDir, t)
		if len(files) > 0 {
			t.Fatalf("FAIL: CNI_CONF_DIR is not empty: %v", files)
		} else {
			t.Log("PASS: CNI_CONF_DIR is empty")
		}
	}

	docker("logs", containerID, t)
	docker("rm", containerID, t)
}

func TestInstallCNI_Scenario1(t *testing.T) {
	t.Log("If the test fails, you will want to check the docker logs of the container and then be sure to stop && remove it before running the tests again.")

	t.Log("Scenario 1: There isn't an existing plugin configuration in the CNI_NET_DIR.")
	t.Log("GIVEN the CNI_NET_DIR=/etc/cni/net.d/ is empty")
	t.Log("WHEN the install-cni.sh script is executed")
	t.Log("THEN it should write the 01-linkerd-cni.conflist file appropriately")
	t.Log("AND WHEN the container is stopped")
	t.Log("THEN it should delete the linkerd-cni artifacts")

	wd := pwd(t)
	t.Logf("..setting the working directory: %v", wd)
	testWd := "/tmp"
	t.Logf("..setting the test working directory: %v", testWd)
	testCNINetDir := mktemp(testWd, "linkerd-cni-confXXXXX", t)
	t.Logf("..creating the test CNI_NET_DIR: %v", testCNINetDir)
	defer rm(testCNINetDir, t)
	testCNIBinDir := mktemp(testWd, "linkerd-cni-binXXXXX", t)
	t.Logf("..creating the test CNI_BIN_DIR: %v", testCNIBinDir)
	defer rm(testCNIBinDir, t)
	testK8sSvcAcctDir := mktemp(testWd, "kube-svcacctXXXXX", t)
	t.Logf("..creating the k8s service account directory: %v", testK8sSvcAcctDir)
	defer rm(testK8sSvcAcctDir, t)

	populateK8sCreds(wd, testK8sSvcAcctDir, t)
	doTest(1, wd, "NONE", "01-linkerd-cni.conflist", wd+"data/expected/01-linkerd-cni.conflist-1", "", testCNINetDir, testCNIBinDir, testK8sSvcAcctDir, testWd, t)
}

func TestInstallCNI_Scenario2(t *testing.T) {
	t.Log("If the test fails, you will want to check the docker logs of the container and then be sure to stop && remove it before running the tests again.")

	t.Log("Scenario 2: There is an existing plugin configuration (.conf) in the CNI_NET_DIR.")
	t.Log("GIVEN the CNI_NET_DIR=/etc/cni/net.d/ is NOT empty")
	t.Log("WHEN the install-cni.sh script is executed")
	t.Log("THEN it should update the existing file contents appropriately")
	t.Log("THEN it should rename the existing file appropriately")
	t.Log("AND WHEN the container is stopped")
	t.Log("THEN it should delete the linkerd-cni artifacts")
	t.Log("THEN it should revert back to the previous plugin configuration and filename")

	wd := pwd(t)
	t.Logf("..setting the working directory: %v", wd)
	testWd := "/tmp"
	t.Logf("..setting the test working directory: %v", testWd)
	testCNINetDir := mktemp(testWd, "linkerd-cni-confXXXXX", t)
	t.Logf("..creating the test CNI_NET_DIR: %v", testCNINetDir)
	defer rm(testCNINetDir, t)
	testCNIBinDir := mktemp(testWd, "linkerd-cni-binXXXXX", t)
	t.Logf("..creating the test CNI_BIN_DIR: %v", testCNIBinDir)
	defer rm(testCNIBinDir, t)
	testK8sSvcAcctDir := mktemp(testWd, "kube-svcacctXXXXX", t)
	t.Logf("..creating the k8s service account directory: %v", testK8sSvcAcctDir)
	defer rm(testK8sSvcAcctDir, t)

	populateTempDirs(wd, testCNINetDir, "10-host-local.conf", t)
	populateK8sCreds(wd, testK8sSvcAcctDir, t)
	doTest(2, wd, hostCniNetDir+"/10-host-local.conf", "10-host-local.conf", wd+"data/expected/10-host-local.conflist-1", wd+"data/expected/10-host-local.conf-1.clean", testCNINetDir, testCNIBinDir, testK8sSvcAcctDir, testWd, t)
}

func TestInstallCNI_Scenario3(t *testing.T) {
	t.Log("If the test fails, you will want to check the docker logs of the container and then be sure to stop && remove it before running the tests again.")

	t.Log("Scenario 3: There is an existing plugin configuration (.conflist) in the CNI_NET_DIR.")
	t.Log("GIVEN the CNI_NET_DIR=/etc/cni/net.d/ is NOT empty")
	t.Log("WHEN the install-cni.sh script is executed")
	t.Log("THEN it should update the existing file contents appropriately")
	t.Log("THEN it should rename the existing file appropriately")
	t.Log("AND WHEN the container is stopped")
	t.Log("THEN it should delete the linkerd-cni artifacts")
	t.Log("THEN it should revert back to the previous plugin configuration and filename")

	wd := pwd(t)
	t.Logf("..setting the working directory: %v", wd)
	testWd := "/tmp"
	t.Logf("..setting the test working directory: %v", testWd)
	testCNINetDir := mktemp(testWd, "linkerd-cni-confXXXXX", t)
	t.Logf("..creating the test CNI_NET_DIR: %v", testCNINetDir)
	defer rm(testCNINetDir, t)
	testCNIBinDir := mktemp(testWd, "linkerd-cni-binXXXXX", t)
	t.Logf("..creating the test CNI_BIN_DIR: %v", testCNIBinDir)
	defer rm(testCNIBinDir, t)
	testK8sSvcAcctDir := mktemp(testWd, "kube-svcacctXXXXX", t)
	t.Logf("..creating the k8s service account directory: %v", testK8sSvcAcctDir)
	defer rm(testK8sSvcAcctDir, t)

	populateTempDirs(wd, testCNINetDir, "10-calico.conflist", t)
	populateK8sCreds(wd, testK8sSvcAcctDir, t)
	doTest(3, wd, hostCniNetDir+"/10-calico.conflist", "10-calico.conflist", wd+"data/expected/10-calico.conflist-1", wd+"data/expected/10-calico.conflist-1.clean", testCNINetDir, testCNIBinDir, testK8sSvcAcctDir, testWd, t)
}
