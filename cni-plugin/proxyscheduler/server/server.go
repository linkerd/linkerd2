package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/julienschmidt/httprouter"
	"github.com/linkerd/linkerd2/cni-plugin/proxyscheduler/api"
	"github.com/sirupsen/logrus"
	"net/http"
)

type ProxySchedulerConfig struct {
	BindPort string
	LinkerdNamespace string
}

type server struct {
	config     *ProxySchedulerConfig
	kubernetes *KubernetesClient
	runtime    *CRIRuntime
}

func NewProxyAgentScheduler(config ProxySchedulerConfig) (*server, error) {
	kube, err := NewKubernetesClient()
	if err != nil {
		return nil, err
	}

	runtime, err := NewCRIRuntime(kube, config.LinkerdNamespace)
	if err != nil {
		return nil, err
	}
	return &server{
		config:     &config,
		runtime:    runtime,
		kubernetes: kube,
	}, nil
}

func (p *server) Run(ctx context.Context) error {
	p.kubernetes.Start(ctx.Done())
	logrus.Info("Starting proxy scheduler")
	logrus.SetLevel(logrus.DebugLevel) //TODO: Make configurable
	router := httprouter.New()
	router.POST("/api/proxy", handleApiError(p.startProxy))
	router.DELETE("/api/proxy/:podnamespace/:podname", handleApiError(stopProxy))

	logrus.Info("Listening on port ", p.config.BindPort)

	err := http.ListenAndServe(fmt.Sprintf(":%v", p.config.BindPort), router)
	if err != nil {
		return err
	}
	return nil
}

func (p *server) startProxy(w http.ResponseWriter, r *http.Request, params httprouter.Params) *apiError {
	startRequest := api.StartProxyRequest{}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&startRequest); err != nil {
		return &apiError{err, "Problem parsing start request", http.StatusBadRequest}
	}
	if err := validateStartRequest(&startRequest, w); err != nil {
		return err
	}

	logrus.Debugf("Starting proxy (podIp:%s, podSandboxId:%s, podName:%s, podNamespace:%s)",
		*startRequest.PodIP, *startRequest.PodSandboxID, *startRequest.PodName, *startRequest.PodNamespace)

	pod, err := p.kubernetes.getPod(*startRequest.PodName, *startRequest.PodNamespace)
	if err != nil {
		return &apiError{err, "Pod cannot be obtained", http.StatusInternalServerError}
	}

	if pod.Status.PodIP == "" {
		pod.Status.PodIP = *startRequest.PodIP
	} else {
		*startRequest.PodIP = pod.Status.PodIP
	}


	if err := p.runtime.StartProxy(*startRequest.PodSandboxID, pod); err != nil {
		return &apiError{err, "Proxy could not start", http.StatusInternalServerError}
	}

	statusResponse(w, http.StatusAccepted)

	defer r.Body.Close()
	return nil
}

func validateStartRequest(req *api.StartProxyRequest, w http.ResponseWriter) *apiError {
	if req.PodIP == nil {
		return &apiError{nil, "pod-ip is missing", http.StatusBadRequest}
	}

	if req.PodNamespace == nil {
		return &apiError{nil, "pod-namespace is missing", http.StatusBadRequest}
	}

	if req.PodName == nil {
		return &apiError{nil, "pod-name is missing", http.StatusBadRequest}
	}

	if req.PodSandboxID == nil {
		return &apiError{nil, "pod-sandbox-id is missing", http.StatusBadRequest}
	}
	return nil
}

func validateStopRequest(req *api.StopProxyRequest, w http.ResponseWriter) *apiError {
	if req.PodSandboxID == nil {
		return &apiError{nil, "pod-sandbox-id is missing", http.StatusBadRequest}
	}
	return nil
}

func stopProxy(w http.ResponseWriter, r *http.Request, p httprouter.Params) *apiError {
	podName := p.ByName("podname")
	podNamespace := p.ByName("podnamespace")

	deleteRequest := api.StopProxyRequest{}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&deleteRequest); err != nil {
		return &apiError{err, "Problem deserializing stop request", http.StatusBadRequest}
	}

	if err := validateStopRequest(&deleteRequest, w); err != nil {
		return err
	}

	logrus.Debugf("Stopping proxy (podSandboxId:%s, podName:%s, podNamespace:%s)",
		deleteRequest.PodSandboxID, podName, podNamespace)

	statusResponse(w, http.StatusOK)
	defer r.Body.Close()
	return nil
}
