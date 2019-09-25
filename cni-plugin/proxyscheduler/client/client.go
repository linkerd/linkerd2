package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/linkerd/linkerd2/cni-plugin/proxyscheduler/api"
	"github.com/sirupsen/logrus"
	"io"
	"net/http"
)

type ProxySchedulerClient struct {
	httpClient *http.Client
	port        int
	log        *logrus.Entry
}

func NewProxyAgentClient(port int, log *logrus.Entry) (*ProxySchedulerClient, error) {
	return &ProxySchedulerClient{
		httpClient: http.DefaultClient,
		port:       port,
		log:        log,
	}, nil
}

func handleResponseError(rsp *http.Response) error {
	if !(rsp.StatusCode >= 200 && rsp.StatusCode < 300) {
		return fmt.Errorf("proxy scheduler returned an error: %v", rsp.Status)
	}
	return nil
}


func (p *ProxySchedulerClient) StartProxy(podName, podNamespace, podIP, infraContainerID string, cniNs string) error {
	httpResponse, err := p.dispatchCallToScheduler(http.MethodPost, "/api/proxy", api.StartProxyRequest {
		&podIP,
		&infraContainerID,
		&podName,
		&podNamespace,
		&cniNs,
	}, nil)

	if err != nil {
		return err
	}
	return handleResponseError(httpResponse)
}

func (p *ProxySchedulerClient) StopProxy(podName, podNamespace, podSandboxID string) error {
	path := fmt.Sprintf("/api/proxy/%s/%s", podNamespace, podName)
	httpResponse, err := p.dispatchCallToScheduler(http.MethodDelete, path, api.StopProxyRequest{&podSandboxID}, nil)

	if err != nil {
		return err
	}
	return handleResponseError(httpResponse)
}


func (p *ProxySchedulerClient) dispatchCallToScheduler(method, path string, request interface{}, responseObj interface{}) (*http.Response, error) {
	var requestBody io.Reader
	if request != nil {
		b, err := json.Marshal(request)
		if err != nil {
			return nil, err
		}
		requestBody = bytes.NewReader(b)
	}

	url := fmt.Sprintf("http://localhost:%v%s", p.port, path)

	p.log.Debugf("Calling scheduler URL %s", url)

	req, err := http.NewRequest(method, url, requestBody)
	if err != nil {
		return nil, err
	}

	response, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if responseObj != nil {
		p.log.Debug("Decoding JSON response")
		decoder := json.NewDecoder(response.Body)
		err := decoder.Decode(responseObj)
		if err != nil {
			return nil, fmt.Errorf("could not decode response: %v", err)
		}
	}

	p.log.Debugf("Agent returned status: %v", response.Status)
	return response, nil
}
