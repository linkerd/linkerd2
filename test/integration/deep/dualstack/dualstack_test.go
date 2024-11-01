package dualstack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
)

type IP struct {
	IP string `json:"ip"`
}

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	// Block test execution until control plane is running
	TestHelper.WaitUntilDeployReady(testutil.LinkerdDeployReplicasEdge)
	os.Exit(m.Run())
}

// TestDualStack creates an injected pod that starts two servers, one listening
// on the IPv4 wildcard address and serving the string "IPv4", and another
// listening on the IPv6 wildcard address and serving the string "IPv6". They
// are fronted by a DualStack Service. We test that we can reach those two IPs
// directly, and that making a request to the service's FQDN always hits the
// IPv6 endpoint.
func TestDualStack(t *testing.T) {
	if !TestHelper.DualStack() {
		t.Skip("Skipping DualStack test")
	}

	TestHelper.WithDataPlaneNamespace(context.Background(), "dualstack-test", map[string]string{}, t, func(t *testing.T, ns string) {
		out, err := TestHelper.Kubectl("",
			"create", "configmap", "go-app",
			"--from-file=main.go=testdata/ipfamilies-server.go",
			"-n", ns,
		)
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v\noutput:\n%s", err, out)
		}

		out, err = TestHelper.Kubectl("",
			"apply", "-f", "testdata/ipfamilies-server-client.yml",
			"-n", ns,
		)
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v\noutput:\n%s", err, out)
		}

		checkPods(t, ns, "ipfamilies-server")
		checkPods(t, ns, "client")

		var clientIPv6, serverIPv4, serverIPv6 string

		t.Run("Retrieve pod IPs", func(t *testing.T) {
			cmd := []string{
				"get", "po",
				"-o", "jsonpath='{.items[*].status.podIPs}'",
				"-n", ns,
			}

			out, err = TestHelper.Kubectl("", append(cmd, "-l", "app=server")...)
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v\noutput:\n%s", err, out)
			}

			var IPs []IP
			out = strings.Trim(out, "'")
			if err = json.Unmarshal([]byte(out), &IPs); err != nil {
				testutil.AnnotatedFatalf(t, "error unmarshaling JSON", "error unmarshaling JSON '%s': %s", out, err)
			}
			if len(IPs) != 2 {
				testutil.AnnotatedFatalf(t, "unexpected number of IPs", "expected 2 IPs, got %s", fmt.Sprint(len(IPs)))
			}
			serverIPv4 = IPs[0].IP
			serverIPv6 = IPs[1].IP

			out, err = TestHelper.Kubectl("", append(cmd, "-l", "app=client")...)
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v\noutput:\n%s", err, out)
			}

			out = strings.Trim(out, "'")
			if err = json.Unmarshal([]byte(out), &IPs); err != nil {
				testutil.AnnotatedFatalf(t, "error unmarshaling JSON", "error unmarshaling JSON '%s': %s", out, err)
			}
			if len(IPs) != 2 {
				testutil.AnnotatedFatalf(t, "unexpected number of IPs", "expected 2 IPs, got %s", fmt.Sprint(len(IPs)))
			}
			clientIPv6 = IPs[1].IP
		})

		t.Run("Apply policy", func(t *testing.T) {
			file, err := os.Open("testdata/ipfamilies-policy.yml")
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v", err)
			}
			defer file.Close()
			manifest, err := io.ReadAll(file)
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v", err)
			}
			in := strings.ReplaceAll(string(manifest), "{IPv6}", clientIPv6)
			out, err = TestHelper.KubectlApply(in, ns)
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v\noutput:\n%s", err, out)
			}
		})

		time.Sleep(10 * time.Second)

		t.Run("Hit IPv4 addr directly", func(t *testing.T) {
			out, err = TestHelper.Kubectl("",
				"exec", "deploy/client",
				"-c", "curl",
				"-n", ns,
				"--",
				"curl", "-s", "-S", "--stderr", "-", "http://"+serverIPv4+":8080",
			)
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v\noutput:\n%s", err, out)
			}
			if out != "IPv4\n" {
				testutil.AnnotatedFatalf(t, "unexpected output", "expected 'IPv4', received '%s'", out)
			}
		})

		t.Run("Hit IPv6 addr directly", func(t *testing.T) {
			out, err = TestHelper.Kubectl("",
				"exec", "deploy/client",
				"-c", "curl",
				"-n", ns,
				"--",
				"curl", "-s", "-S", "--stderr", "-", "http://["+serverIPv6+"]:8080",
			)
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v\noutput:\n%s", err, out)
			}
			if out != "IPv6\n" {
				testutil.AnnotatedFatalf(t, "expected 'IPv6', received '%s'", out)
			}
		})

		t.Run("Hit FQDN directly (should always resolve to IPv6)", func(t *testing.T) {
			for i := 0; i < 10; i++ {
				out, err = TestHelper.Kubectl("",
					"exec", "deploy/client",
					"-c", "curl",
					"-n", ns,
					"--",
					"curl", "-s", "-S", "--stderr", "-", "http://ipfamilies-server:8080",
				)
				if err != nil {
					testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v\noutput:\n%s", err, out)
				}
				if out != "IPv6\n" {
					testutil.AnnotatedFatalf(t, "expected 'IPv6', received '%s'", out)
				}
			}
		})
	})
}

func checkPods(t *testing.T, ns, pod string) {
	t.Helper()

	if err := TestHelper.CheckPods(context.Background(), ns, pod, 1); err != nil {
		//nolint:errorlint
		if rce, ok := err.(*testutil.RestartCountError); ok {
			testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
		} else {
			testutil.AnnotatedError(t, "CheckPods timed-out", err)
		}
	}
}
