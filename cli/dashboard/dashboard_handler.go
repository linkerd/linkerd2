package dashboard

import (
	"fmt"
	"net/http"
	"strings"

	healthcheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
	"github.com/runconduit/conduit/pkg/healthcheck"
	"github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
)

const ConduitDashboardSubsystemName = "Conduit Dashboard"
const ConduitDashboardCheckDescription = "can query the conduit dashboard"

type DashboardHandler struct {
	kubeApi k8s.KubernetesApi
	k8s.Kubectl
	healthcheck.StatusChecker
}

func NewDashboardHandler(kubectl k8s.Kubectl, kubeApi k8s.KubernetesApi) *DashboardHandler {
	return &DashboardHandler{Kubectl: kubectl, kubeApi: kubeApi}
}

func (d *DashboardHandler) SelfCheck() []*healthcheckPb.CheckResult {
	client, err := d.kubeApi.NewClient()
	if err != nil {
		log.Fatalf("error instantiating Kubernetes API client: %v", err)
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
		log.Fatalf("Failed HTTP GET Request to dashboard endpoint: %v", err)
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
