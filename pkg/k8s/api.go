package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/rest"
	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const (
	KubeapiSubsystemName           = "kubernetes-api"
	KubeapiClientCheckDescription  = "can initialize the client"
	KubeapiAccessCheckDescription  = "can query the Kubernetes API"
	KubeapiVersionCheckDescription = "is running the minimum Kubernetes API version"
)

var minApiVersion = [3]int{1, 8, 0}

type KubernetesApi interface {
	UrlFor(namespace string, extraPathStartingWithSlash string) (*url.URL, error)
	NewClient() (*http.Client, error)
	healthcheck.StatusChecker
}

type kubernetesApi struct {
	*rest.Config
}

func (kubeapi *kubernetesApi) NewClient() (*http.Client, error) {
	secureTransport, err := rest.TransportFor(kubeapi.Config)
	if err != nil {
		return nil, fmt.Errorf("error instantiating Kubernetes API client: %v", err)
	}

	return &http.Client{
		Transport: secureTransport,
	}, nil
}

func (kubeapi *kubernetesApi) SelfCheck() (checks []*healthcheckPb.CheckResult) {
	apiConnectivityCheck, client := kubeapi.checkApiConnectivity()
	checks = append(checks, apiConnectivityCheck)
	if apiConnectivityCheck.Status != healthcheckPb.CheckStatus_OK {
		return
	}

	apiAccessCheck, versionRsp := kubeapi.checkApiAccess(client)
	checks = append(checks, apiAccessCheck)
	if apiAccessCheck.Status != healthcheckPb.CheckStatus_OK {
		return
	}

	checks = append(checks, kubeapi.checkApiVersion(versionRsp))
	return
}

func (kubeapi *kubernetesApi) checkApiConnectivity() (*healthcheckPb.CheckResult, *http.Client) {
	checkResult := &healthcheckPb.CheckResult{
		Status:           healthcheckPb.CheckStatus_OK,
		SubsystemName:    KubeapiSubsystemName,
		CheckDescription: KubeapiClientCheckDescription,
	}

	client, err := kubeapi.NewClient()
	if err != nil {
		checkResult.Status = healthcheckPb.CheckStatus_ERROR
		checkResult.FriendlyMessageToUser = fmt.Sprintf("Error connecting to the API. Error message is [%s]", err.Error())
		return checkResult, client
	}

	return checkResult, client
}

func (kubeapi *kubernetesApi) checkApiAccess(client *http.Client) (*healthcheckPb.CheckResult, string) {
	checkResult := &healthcheckPb.CheckResult{
		Status:           healthcheckPb.CheckStatus_OK,
		SubsystemName:    KubeapiSubsystemName,
		CheckDescription: KubeapiAccessCheckDescription,
	}

	endpointToCheck, err := url.Parse(kubeapi.Host + "/version")
	if err != nil {
		checkResult.Status = healthcheckPb.CheckStatus_ERROR
		checkResult.FriendlyMessageToUser = fmt.Sprintf("Error configuring the Kubernetes API host %s: %s", kubeapi.Host, err)
		return checkResult, ""
	}

	req, _ := http.NewRequest("GET", endpointToCheck.String(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		checkResult.Status = healthcheckPb.CheckStatus_ERROR
		checkResult.FriendlyMessageToUser = fmt.Sprintf("Error calling the Kubernetes API: %s", err)
		return checkResult, ""
	}
	defer resp.Body.Close()

	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		checkResult.Status = healthcheckPb.CheckStatus_ERROR
		checkResult.FriendlyMessageToUser = fmt.Sprintf("Error reading Kubernetes API response body: %s", err)
		return checkResult, ""
	}
	body := string(bytes)

	if resp.StatusCode != http.StatusOK {
		checkResult.Status = healthcheckPb.CheckStatus_FAIL
		checkResult.FriendlyMessageToUser = fmt.Sprintf("Unexpected Kubernetes API response: %s, body: %s", resp.Status, body)
		return checkResult, ""
	}

	return checkResult, body
}

func (kubeapi *kubernetesApi) checkApiVersion(versionRsp string) *healthcheckPb.CheckResult {
	checkResult := &healthcheckPb.CheckResult{
		Status:           healthcheckPb.CheckStatus_OK,
		SubsystemName:    KubeapiSubsystemName,
		CheckDescription: KubeapiVersionCheckDescription,
	}

	var versionInfo version.Info
	err := json.Unmarshal([]byte(versionRsp), &versionInfo)
	if err != nil {
		checkResult.Status = healthcheckPb.CheckStatus_ERROR
		checkResult.FriendlyMessageToUser = fmt.Sprintf("Version endpoint returned invalid JSON: [%v]", versionRsp)
		return checkResult
	}

	apiVersion, err := getK8sVersion(versionInfo.String())
	if err != nil {
		checkResult.Status = healthcheckPb.CheckStatus_ERROR
		checkResult.FriendlyMessageToUser = fmt.Sprintf("Failed to parse version [%s]: %s", versionInfo.String(), err)
		return checkResult
	}

	if !isCompatibleVersion(minApiVersion, apiVersion) {
		checkResult.Status = healthcheckPb.CheckStatus_FAIL
		checkResult.FriendlyMessageToUser = fmt.Sprintf("Kubernetes is on version [%d.%d.%d], but version [%d.%d.%d] or more recent is required.",
			apiVersion[0], apiVersion[1], apiVersion[2],
			minApiVersion[0], minApiVersion[1], minApiVersion[2])
		return checkResult
	}

	return checkResult
}

// UrlFor generates a URL based on the Kubernetes config.
func (kubeapi *kubernetesApi) UrlFor(namespace string, extraPathStartingWithSlash string) (*url.URL, error) {
	return generateKubernetesApiBaseUrlFor(kubeapi.Host, namespace, extraPathStartingWithSlash)
}

// NewAPI returns a new KubernetesApi interface
func NewAPI(configPath string) (KubernetesApi, error) {
	config, err := getConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("error configuring Kubernetes API client: %v", err)
	}

	return &kubernetesApi{Config: config}, nil
}
