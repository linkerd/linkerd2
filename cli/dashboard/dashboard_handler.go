package dashboard

import (
	"fmt"
	"net/http"
	"strings"

	healthcheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
	"github.com/runconduit/conduit/pkg/healthcheck"
	"github.com/runconduit/conduit/pkg/k8s"
	"github.com/runconduit/conduit/pkg/shell"
	log "github.com/sirupsen/logrus"
)

const ConduitDashboardSubsystemName = "Conduit Dashboard"
const ConduitDashboardCheckDescription = "can query the conduit dashboard"

type DashboardHandler struct {
	kubeApi k8s.KubernetesApi
	k8s.Kubectl
	healthcheck.StatusChecker
}

func NewDashboardHandler(kubeconfigPath string) (*DashboardHandler, error) {
	sh := shell.NewUnixShell()
	kubectl, err := k8s.NewKubectl(sh)
	if err != nil {
		return nil, fmt.Errorf("failed to start kubectl: %v", err)
	}

	kubeApi, err := k8s.NewK8sAPI(sh, kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the Kubernetes API: %v", err)
	}
	return &DashboardHandler{Kubectl: kubectl, kubeApi: kubeApi}, nil
}

func (d *DashboardHandler) SelfCheck() []*healthcheckPb.CheckResult {
	client, err := d.kubeApi.NewClient()
	if err != nil {
		log.Fatalf("error instantiating Kubernetes API clien: %v", err)
	}
	dashboardCheckResult := d.checkDashboardAccess(client)
	return []*healthcheckPb.CheckResult{dashboardCheckResult}
}

func (d *DashboardHandler) checkDashboardAccess(client *http.Client) *healthcheckPb.CheckResult {
	checkResult := &healthcheckPb.CheckResult{
		Status:           healthcheckPb.CheckStatus_OK,
		SubsystemName:    ConduitDashboardSubsystemName,
		CheckDescription: ConduitDashboardCheckDescription,
	}
	dashboardEndpoint, err := d.kubeApi.UrlFor("conduit", "/services/web:http/proxy")
	if err != nil {
		checkResult.Status = healthcheckPb.CheckStatus_ERROR
		checkResult.FriendlyMessageToUser = "Failed to generate dashboard URL"
		return checkResult
	}
	resp, err := client.Get(dashboardEndpoint.String())
	if err != nil {
		log.Fatalf("Failed HTTP GET Request to dashboardEndpoint: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		checkResult.Status = healthcheckPb.CheckStatus_FAIL
		checkResult.FriendlyMessageToUser = fmt.Sprintf("HTTP GET request to endpoint [%s] resulted in invalid response [%v]", dashboardEndpoint, resp.Status)
		return checkResult
	}
	return checkResult
}

func (d *DashboardHandler) IsDashboardAvailable() bool {
	checkResults := d.SelfCheck()
	for _, checkResult := range checkResults {
		if strings.Contains(checkResult.SubsystemName, ConduitDashboardSubsystemName) {
			return checkResult.Status == healthcheckPb.CheckStatus_OK
		}
	}
	return false
}
