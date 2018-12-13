package iptablestest

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

const (
	ignoredContainerPort     = "7070"
	proxyContainerPort       = "8080"
	notTheProxyContainerPort = "9090"
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
		expectSuccessfulGetRequestTo(t, svcName, proxyContainerPort)
	})

	t.Run("fails to connect to pod via a service through any port that isn't the container's exposed port", func(t *testing.T) {
		expectCannotConnectGetRequestTo(t, svcName, "8088")
		expectCannotConnectGetRequestTo(t, svcName, "8888")
		expectCannotConnectGetRequestTo(t, svcName, "8988")
	})
}

func TestPodWithSomePortsRedirected(t *testing.T) {
	t.Parallel()

	podRedirectsSomePortsIP := os.Getenv("POD_REDIRECTS_WHITELISTED_IP")

	t.Run("succeeds connecting to pod directly through container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequestTo(t, podRedirectsSomePortsIP, proxyContainerPort)
	})

	t.Run("succeeds connecting to pod directly through ports configured to redirect", func(t *testing.T) {
		expectSuccessfulGetRequestTo(t, podRedirectsSomePortsIP, "9090")
		expectSuccessfulGetRequestTo(t, podRedirectsSomePortsIP, "9099")
	})

	t.Run("fails to connect to pod via through any port that isn't configured to redirect", func(t *testing.T) {
		expectCannotConnectGetRequestTo(t, podRedirectsSomePortsIP, "8088")
		expectCannotConnectGetRequestTo(t, podRedirectsSomePortsIP, "8888")
		expectCannotConnectGetRequestTo(t, podRedirectsSomePortsIP, "8988")
	})
}

func TestPodWithSomePortsIgnored(t *testing.T) {
	t.Parallel()

	podIgnoredSomePortsIP := os.Getenv("POD_DOEST_REDIRECT_BLACKLISTED_IP")

	t.Run("succeeds connecting to pod directly through container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequestTo(t, podIgnoredSomePortsIP, proxyContainerPort)
	})

	t.Run("succeeds connecting to pod directly through ports configured to redirect", func(t *testing.T) {
		expectSuccessfulGetRequestTo(t, podIgnoredSomePortsIP, "9090")
		expectSuccessfulGetRequestTo(t, podIgnoredSomePortsIP, "9099")
	})

	t.Run("doesnt redirect when through port that is ignored", func(t *testing.T) {
		response := expectSuccessfulGetRequestTo(t, podIgnoredSomePortsIP, ignoredContainerPort)

		if response == "proxy" {
			t.Fatalf("Expected connection through ignored port to directly hit service, but hit [%s]", response)
		}

		if !strings.Contains(response, ignoredContainerPort) {
			t.Fatalf("Expected to be able to connect to %s without redirects, but got back %s", ignoredContainerPort, response)
		}
	})
}

func TestPodMakesOutboundConnection(t *testing.T) {
	t.Parallel()

	podIgnoredSomePortsIP := os.Getenv("POD_DOEST_REDIRECT_BLACKLISTED_IP")
	podWithNoRulesIP := os.Getenv("POD_WITH_NO_RULES_IP")
	podWithNoRulesName := "pod-with-no-rules"

	proxyPodName := "pod-doesnt-redirect-blacklisted"
	proxyPodIP := podIgnoredSomePortsIP

	t.Run("connecting to another pod from non-proxy container gets redirected to proxy", func(t *testing.T) {
		portOfContainerToMAkeTheRequest := ignoredContainerPort
		targetPodIP := podWithNoRulesIP
		targetPort := ignoredContainerPort

		response := makeCallFromContainerToAnother(t, proxyPodIP, portOfContainerToMAkeTheRequest, targetPodIP, targetPort)

		expectedDownstreamResponse := fmt.Sprintf("me:[%s:%s]downstream:[proxy]", proxyPodName, portOfContainerToMAkeTheRequest)
		if !strings.Contains(response, expectedDownstreamResponse) {
			t.Fatalf("Expected response to be redirected to the proxy, expected %s but it was %s", expectedDownstreamResponse, response)
		}
	})

	t.Run("connecting to another pod from proxy container does not get redirected to proxy", func(t *testing.T) {
		targetPodName := podWithNoRulesName
		targetPodIP := podWithNoRulesIP

		response := makeCallFromContainerToAnother(t, proxyPodIP, proxyContainerPort, targetPodIP, notTheProxyContainerPort)

		expectedDownstreamResponse := fmt.Sprintf("me:[proxy]downstream:[%s:%s]", targetPodName, notTheProxyContainerPort)
		if !strings.Contains(response, expectedDownstreamResponse) {
			t.Fatalf("Expected response not to be redirected to the proxy, expected %s but it was %s", expectedDownstreamResponse, response)
		}
	})

	t.Run("connecting to loopback from non-proxy container does not get redirected to proxy", func(t *testing.T) {
		response := makeCallFromContainerToAnother(t, proxyPodIP, ignoredContainerPort, "127.0.0.1", notTheProxyContainerPort)

		expectedDownstreamResponse := fmt.Sprintf("me:[%s:%s]downstream:[%s:%s]", proxyPodName, ignoredContainerPort, proxyPodName, notTheProxyContainerPort)
		if !strings.Contains(response, expectedDownstreamResponse) {
			t.Fatalf("Expected response not to be redirected to the proxy, expected %s but it was %s", expectedDownstreamResponse, response)
		}
	})
}

func makeCallFromContainerToAnother(t *testing.T, fromPodNamed string, fromContainerAtPort string, podIWantToReachName string, containerPortIWantToReach string) string {
	downstreamURL := fmt.Sprintf("http://%s:%s", podIWantToReachName, containerPortIWantToReach)

	//Make request asking target to make a back-end request
	targetURL := fmt.Sprintf("http://%s:%s/call?url=%s", fromPodNamed, fromContainerAtPort, url.QueryEscape(downstreamURL))
	return expectSuccessfulGetRequestToURL(t, targetURL)
}

func expectCannotConnectGetRequestTo(t *testing.T, host string, port string) {
	targetURL := fmt.Sprintf("http://%s:%s/", host, port)
	fmt.Printf("Expecting failed GET to %s\n", targetURL)
	resp, err := http.Get(targetURL)
	if err == nil {
		t.Fatalf("Expected error when connecting to %s, got:\n%+v", targetURL, resp)
	}
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
