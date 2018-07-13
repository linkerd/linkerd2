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

	podWithNoRulesIp := os.Getenv("POD_WITH_NO_RULES_IP")
	svcName := "svc-pod-with-no-rules"

	t.Run("succeeds connecting to pod directly through container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequestTo(t, podWithNoRulesIp, proxyContainerPort)
	})

	t.Run("fails to connect to pod directly through any port that isn't the container's exposed port", func(t *testing.T) {
		expectCannotConnectGetRequestTo(t, podWithNoRulesIp, "8088")
		expectCannotConnectGetRequestTo(t, podWithNoRulesIp, "8888")
		expectCannotConnectGetRequestTo(t, podWithNoRulesIp, "8988")
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

	podRedirectsAllPortsIp := os.Getenv("POD_REDIRECTS_ALL_PORTS_IP")
	svcName := "svc-pod-redirects-all-ports"

	t.Run("succeeds connecting to pod directly through container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequestTo(t, podRedirectsAllPortsIp, proxyContainerPort)
	})

	t.Run("succeeds connecting to pod directly through any port that isn't the container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequestTo(t, podRedirectsAllPortsIp, "8088")
		expectSuccessfulGetRequestTo(t, podRedirectsAllPortsIp, "8888")
		expectSuccessfulGetRequestTo(t, podRedirectsAllPortsIp, "8988")

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

	podRedirectsSomePortsIp := os.Getenv("POD_REDIRECTS_WHITELISTED_IP")

	t.Run("succeeds connecting to pod directly through container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequestTo(t, podRedirectsSomePortsIp, proxyContainerPort)
	})

	t.Run("succeeds connecting to pod directly through ports configured to redirect", func(t *testing.T) {
		expectSuccessfulGetRequestTo(t, podRedirectsSomePortsIp, "9090")
		expectSuccessfulGetRequestTo(t, podRedirectsSomePortsIp, "9099")
	})

	t.Run("fails to connect to pod via through any port that isn't configured to redirect", func(t *testing.T) {
		expectCannotConnectGetRequestTo(t, podRedirectsSomePortsIp, "8088")
		expectCannotConnectGetRequestTo(t, podRedirectsSomePortsIp, "8888")
		expectCannotConnectGetRequestTo(t, podRedirectsSomePortsIp, "8988")
	})
}

func TestPodWithSomePortsIgnored(t *testing.T) {
	t.Parallel()

	podIgnoredSomePortsIp := os.Getenv("POD_DOEST_REDIRECT_BLACKLISTED_IP")

	t.Run("succeeds connecting to pod directly through container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequestTo(t, podIgnoredSomePortsIp, proxyContainerPort)
	})

	t.Run("succeeds connecting to pod directly through ports configured to redirect", func(t *testing.T) {
		expectSuccessfulGetRequestTo(t, podIgnoredSomePortsIp, "9090")
		expectSuccessfulGetRequestTo(t, podIgnoredSomePortsIp, "9099")
	})

	t.Run("doesnt redirect when through port that is ignored", func(t *testing.T) {
		response := expectSuccessfulGetRequestTo(t, podIgnoredSomePortsIp, ignoredContainerPort)

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

	podIgnoredSomePortsIp := os.Getenv("POD_DOEST_REDIRECT_BLACKLISTED_IP")
	podWithNoRulesIp := os.Getenv("POD_WITH_NO_RULES_IP")
	podWithNoRulesName := "pod-with-no-rules"

	proxyPodName := "pod-doesnt-redirect-blacklisted"
	proxyPodIp := podIgnoredSomePortsIp

	t.Run("connecting to another pod from non-proxy container gets redirected to proxy", func(t *testing.T) {
		portOfContainerToMAkeTheRequest := ignoredContainerPort
		targetPodIp := podWithNoRulesIp
		targetPort := ignoredContainerPort

		response := makeCallFromContainerToAnother(t, proxyPodIp, portOfContainerToMAkeTheRequest, targetPodIp, targetPort)

		expectedDownstreamResponse := fmt.Sprintf("me:[%s:%s]downstream:[proxy]", proxyPodName, portOfContainerToMAkeTheRequest)
		if !strings.Contains(response, expectedDownstreamResponse) {
			t.Fatalf("Expected response to be redirected to the proxy, expected %s but it was %s", expectedDownstreamResponse, response)
		}
	})

	t.Run("connecting to another pod from proxy container does not get redirected to proxy", func(t *testing.T) {
		targetPodName := podWithNoRulesName
		targetPodIp := podWithNoRulesIp

		response := makeCallFromContainerToAnother(t, proxyPodIp, proxyContainerPort, targetPodIp, notTheProxyContainerPort)

		expectedDownstreamResponse := fmt.Sprintf("me:[proxy]downstream:[%s:%s]", targetPodName, notTheProxyContainerPort)
		if !strings.Contains(response, expectedDownstreamResponse) {
			t.Fatalf("Expected response not to be redirected to the proxy, expected %s but it was %s", expectedDownstreamResponse, response)
		}
	})

	t.Run("connecting to loopback from non-proxy container does not get redirected to proxy", func(t *testing.T) {
		response := makeCallFromContainerToAnother(t, proxyPodIp, ignoredContainerPort, "127.0.0.1", notTheProxyContainerPort)

		expectedDownstreamResponse := fmt.Sprintf("me:[%s:%s]downstream:[%s:%s]", proxyPodName, ignoredContainerPort, proxyPodName, notTheProxyContainerPort)
		if !strings.Contains(response, expectedDownstreamResponse) {
			t.Fatalf("Expected response not to be redirected to the proxy, expected %s but it was %s", expectedDownstreamResponse, response)
		}
	})
}

func makeCallFromContainerToAnother(t *testing.T, fromPodNamed string, fromContainerAtPort string, podIWantToReachName string, containerPortIWantToReach string) string {
	downstreamUrl := fmt.Sprintf("http://%s:%s", podIWantToReachName, containerPortIWantToReach)

	//Make request asking target to make a back-end request
	targetUrl := fmt.Sprintf("http://%s:%s/call?url=%s", fromPodNamed, fromContainerAtPort, url.QueryEscape(downstreamUrl))
	return expectSuccessfulGetRequestToUrl(t, targetUrl)
}

func expectCannotConnectGetRequestTo(t *testing.T, host string, port string) {
	targetUrl := fmt.Sprintf("http://%s:%s/", host, port)
	fmt.Printf("Expecting failed GET to %s\n", targetUrl)
	resp, err := http.Get(targetUrl)
	if err == nil {
		t.Fatalf("Expected error when connecting to %s, got:\n%+v", targetUrl, resp)
	}
}

func expectSuccessfulGetRequestTo(t *testing.T, host string, port string) string {
	targetUrl := fmt.Sprintf("http://%s:%s/", host, port)

	return expectSuccessfulGetRequestToUrl(t, targetUrl)
}

func expectSuccessfulGetRequestToUrl(t *testing.T, url string) string {
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
