package target3

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

var (
	tcpConnRE = regexp.MustCompile(
		`tcp_open_total\{direction="outbound",peer="dst",target_addr="[0-9\.]+:[0-9]+,target_ip="[0-9\.]+",target_port="[0-9]+",tls="true",server_id="default\.multicluster-statefulset\.serviceaccount\.identity\.linkerd\.cluster\.local",dst_control_plane_ns="linkerd",dst_namespace="multicluster-statefulset",dst_pod="nginx-statefulset-0",dst_serviceaccount="default",dst_statefulset="nginx-statefulset"} [0-9]+`,
	)
    httpReqRE = regexp.MustCompile(str string)

)

/*
request_total{direction="outbound",target_addr="10.42.0.15:8080",target_ip="10.42.0.15",target_port="8080",tls="true",server_id="default.multicluster-statefulset.serviceaccount.identity.linkerd.target.cluster.local",dst_control_plane_ns="linkerd",dst_namespace="multicluster-statefulset",dst_pod="nginx-statefulset-0",dst_serviceaccount="default",dst_statefulset="nginx-statefulset"}
*/
func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	if !TestHelper.Multicluster() {
		fmt.Fprintln(os.Stderr, "Multicluster test disabled")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// TestSetupNginx applies the nginx-statefulset.yml manifest in the target
//cluster in the "default" namespace, and mirrors nginx-svc to source cluster
//
func TestMulticlusterTargetTraffic(t *testing.T) {
	out, err := TestHelper.Kubectl("", "config", "use-context", "k3d-target")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to switch context", "failed to switch context: %s\n%s", err, out)
	}
	err = TestHelper.CreateDataPlaneNamespaceIfNotExists(context.Background(),
		"multicluster-statefulset", nil)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to create test namespace", "failed to create test namespace: %s", err)
	}

	nginx, err := TestHelper.LinkerdRun("inject", "testdata/nginx.yaml")
	if err != nil {
		testutil.AnnotatedFatal(t, "unexpected error", err)
	}

	out, err = TestHelper.KubectlApply(nginx, "")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to set up nginx resources", "failed to set up nginx resources: %s\n%s", err, out)
	}

	out, err = TestHelper.Kubectl("", "config", "use-context", "k3d-source")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to switch context", "failed to switch context: %s\n%s", err, out)
	}

	err = TestHelper.CreateDataPlaneNamespaceIfNotExists(context.Background(),
		"multicluster-statefulset", nil)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to create test namespace", "failed to create test namespace: %s", err)
	}

	slowcooker, err := TestHelper.LinkerdRun("inject", "testdata/slow-cooker.yaml")
	if err != nil {
		testutil.AnnotatedFatal(t, "unexpected error", err)
	}
	out, err = TestHelper.KubectlApply(slowcooker, "")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to set up slow-cooker resources", "failed to set up slow-cooker resources: %s\n%s", err, out)
	}

}
