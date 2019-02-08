package iptablestest

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"testing"
  "time"
)

const (
	proxyContainerPort       = "8080"
  attemptToRetry           = "attemptToRetry"
  retryLimit               = 10
)

func TestMain(m *testing.M) {
	runTests := flag.Bool("integration-tests", false, "must be provided to run the integration tests")
	flag.Parse()

	if !*runTests {
		fmt.Fprintln(os.Stderr, "integration tests not enabled: enable with -integration-tests")
		os.Exit(0)
	}

	os.Exit(m.Run())
}

func TestPodWithNoRules(t *testing.T) {
	t.Parallel()

	podWithNoRulesIP := os.Getenv("POD_WITH_NO_RULES_IP")
	svcName := "svc-pod-with-no-rules"

	t.Run("succeeds connecting to pod directly through container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequestTo(t, podWithNoRulesIP, proxyContainerPort)
	})

	t.Run("fails to connect to pod directly through any port that isn't the container's exposed port", func(t *testing.T) {
		expectCannotConnectGetRequestTo(t, podWithNoRulesIP, "8088")
		expectCannotConnectGetRequestTo(t, podWithNoRulesIP, "8888")
		expectCannotConnectGetRequestTo(t, podWithNoRulesIP, "8988")
	})

	t.Run("succeeds connecting to pod via a service through container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequestTo(t, svcName, proxyContainerPort)
	})

	t.Run("fails to connect to pod via a service through any port that isn't the container's exposed port", func(t *testing.T) {
		expectCannotConnectGetRequestTo(t, svcName, "8088")
		expectCannotConnectGetRequestTo(t, svcName, "8888")
		expectCannotConnectGetRequestTo(t, svcName, "8988")
	})
}

func TestPodRedirectsAllPorts(t *testing.T) {
	t.Parallel()
	podRedirectsAllPortsIP := os.Getenv("POD_REDIRECTS_ALL_PORTS_IP")
	svcName := "svc-pod-redirects-all-ports"

	t.Run("succeeds connecting to pod directly through container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequestTo(t, podRedirectsAllPortsIP, proxyContainerPort)
	})

	t.Run("succeeds connecting to pod directly through any port that isn't the container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequestTo(t, podRedirectsAllPortsIP, "8088")
		expectSuccessfulGetRequestTo(t, podRedirectsAllPortsIP, "8888")
		expectSuccessfulGetRequestTo(t, podRedirectsAllPortsIP, "8988")
	})

	t.Run("succeeds connecting to pod via a service through container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequestToRetry(t, svcName, proxyContainerPort)
	})

	t.Run("fails to connect to pod via a service through any port that isn't the container's exposed port", func(t *testing.T) {
		expectCannotConnectGetRequestTo(t, svcName, "8088")
		expectCannotConnectGetRequestTo(t, svcName, "8888")
		expectCannotConnectGetRequestTo(t, svcName, "8988")
	})
}

func expectCannotConnectGetRequestTo(t *testing.T, host string, port string) {
	targetURL := fmt.Sprintf("http://%s:%s/", host, port)
	fmt.Printf("Expecting failed GET to %s\n", targetURL)
	resp, err := http.Get(targetURL)
	if err == nil {
		t.Fatalf("Expected error when connecting to %s, got:\n%+v", targetURL, resp)
	}
}

func expectSuccessfulGetRequestToRetry(t *testing.T, host string, port string) string {
	targetURL := fmt.Sprintf("http://%s:%s/", host, port)

	var result string = expectSuccessfulGetRequestToURLRetry(t, targetURL)
  var count int

  for result == attemptToRetry && count < retryLimit {
    fmt.Printf("Request failed. Retrying request. Attempt %d of %d\n", count+1, retryLimit)
    time.Sleep(2 * time.Second)
    result =  expectSuccessfulGetRequestToURLRetry(t, targetURL)
    count = count + 1
  }
  if result == attemptToRetry {
    t.Fatalf("failed to send HTTP GET to %s:\n", targetURL)
  }
  return result
}

func expectSuccessfulGetRequestToURLRetry(t *testing.T, url string) string {
	fmt.Printf("Expecting successful GET to %s\n", url)
	resp, err := http.Get(url)
	if err != nil {
    fmt.Printf("fail to %s with %v", url, err)
    return attemptToRetry
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed reading GET response from %s:\n%v", url, err)
	}
	response := string(body)
	fmt.Printf("Response from %s: %s", url, response)
	return response
}

func expectSuccessfulGetRequestTo(t *testing.T, host string, port string) string {
	targetURL := fmt.Sprintf("http://%s:%s/", host, port)
	return expectSuccessfulGetRequestToURL(t, targetURL)
}

func expectSuccessfulGetRequestToURL(t *testing.T, url string) string {
	fmt.Printf("Expecting successful GET to %s\n", url)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("failed to send HTTP GET to %s:\n%v", url, err)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed reading GET response from %s:\n%v", url, err)
	}
	response := string(body)
	fmt.Printf("Response from %s: %s", url, response)
	return response
}
