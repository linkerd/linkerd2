package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/julienschmidt/httprouter"
	"github.com/linkerd/linkerd2/cni-plugin/proxyscheduler/api"
	"github.com/sirupsen/logrus"
	"net/http"
	"k8s.io/apimachinery/pkg/types"
	"time"
)

type ProxySchedulerConfig struct {
	BindPort string
	LinkerdNamespace string
}

type server struct {
	config     *ProxySchedulerConfig
	kubernetes *KubernetesClient
	runtime    *CRIRuntime
	healthMonitor *HealthMonitor
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

	monitor := NewHealthMonitor(runtime, kube)
	monitor.StartHealthMonitor()

	return &server{
		config:     &config,
		runtime:    runtime,
		kubernetes: kube,
		healthMonitor : monitor,
	}, nil
}

func (p *server) Run(ctx context.Context) error {
	p.kubernetes.Start(ctx.Done())
	logrus.Info("Starting proxy scheduler")
	logrus.SetLevel(logrus.DebugLevel) //TODO: Make configurable
	router := httprouter.New()
	router.POST("/api/proxy", handleApiError(p.startProxy))
	router.GET("/api/proxy/ready", handleApiError(p.checkReady))
	router.DELETE("/api/proxy/:podnamespace/:podname", handleApiError(stopProxy))

	logrus.Info("Listening on port ", p.config.BindPort)

	err := http.ListenAndServe(fmt.Sprintf(":%v", p.config.BindPort), router)
	if err != nil {
		return err
	}
	return nil
}

func (p *server) checkReady(w http.ResponseWriter, r *http.Request, params httprouter.Params) *apiError {
	rdnsRequest := api.ReadinessCheckRequest{}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&rdnsRequest); err != nil {
		return &apiError{err, "Problem parsing readiness check  request", http.StatusBadRequest}
	}
	if err := validateReadinessCheckRequest(&rdnsRequest, w); err != nil {
		return err
	}

	proxyReady, err := IsProxyReady(*rdnsRequest.PodName, *rdnsRequest.PodNamespace, *rdnsRequest.PodIP,*rdnsRequest.CniNs)
	if err != nil {
		return &apiError{err, "Could not verify proxy readiness", http.StatusInternalServerError}
	}
	if proxyReady {
		statusResponse(w, http.StatusOK)
	} else {
		statusResponse(w, http.StatusNotFound)
	}
	return nil
}



func (p *server) startProxy(w http.ResponseWriter, r *http.Request, params httprouter.Params) *apiError {
	startRequest := api.StartProxyRequest{}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&startRequest); err != nil {
		return &apiError{err, "Problem parsing start request", http.StatusBadRequest}
	}

	logEntry := logrus.WithFields(logrus.Fields{
		"Pod":         *startRequest.PodName,
		"Namespace":   *startRequest.PodNamespace,
	})

	if err := validateStartRequest(&startRequest, w); err != nil {
		return err
	}
	logEntry.Debug("Starting proxy")
	pod, err := p.kubernetes.getPod(*startRequest.PodName, *startRequest.PodNamespace)
	if err != nil {
		return &apiError{err, "Pod cannot be obtained", http.StatusInternalServerError}
	}

	if pod.Status.PodIP == "" {
		pod.Status.PodIP = *startRequest.PodIP
	} else {
		*startRequest.PodIP = pod.Status.PodIP
	}

	if err := p.runtime.StartProxy(*startRequest.PodSandboxID, pod, logEntry); err != nil {
		return &apiError{err, "Proxy could not start", http.StatusInternalServerError}
	}

	// dispatch that asynchronously so client does not wait for response
	//TODO: Handle potential errors
	//TODO: Add timeout + cleanup
	go p.registerProxyForHealthMonitoring(&startRequest, pod.UID, logEntry)
	statusResponse(w, http.StatusAccepted)
	defer r.Body.Close()
	return nil
}


func (p *server)  registerProxyForHealthMonitoring(startRequest *api.StartProxyRequest, podUUID types.UID, logEntry *logrus.Entry)  {
	waitFor := ReadinessCheckInitialDelayMs * time.Millisecond
	logEntry.Debugf("Waiting for %s before checking whether pod %s is ready", waitFor, podUUID)
	time.Sleep(waitFor) // wait a bit before trying...
	for start := time.Now(); time.Since(start) < time.Second * 20; {
		ready, err := IsProxyReady(*startRequest.PodName,*startRequest.PodNamespace, *startRequest.PodIP ,*startRequest.CniNs)
		if err != nil {
			logEntry.Errorf("Could not perform readiness check: %v", err)
		}
		if ready {
			logEntry.Debugf("Proxy for pod %s is ready", podUUID)
			if err := p.healthMonitor.StartMonitoringProxyHealth(*startRequest.PodNamespace,*startRequest.PodName, *startRequest.CniNs ); err != nil {
				logEntry.Errorf("Could not register proxy for pod %s with health manager", podUUID)
			}
			break
		}
	}

}

func validateReadinessCheckRequest(req *api.ReadinessCheckRequest, w http.ResponseWriter) *apiError {
	if req.PodIP == nil {
		return &apiError{nil, "pod-ip is missing", http.StatusBadRequest}
	}

	if req.PodNamespace == nil {
		return &apiError{nil, "pod-namespace is missing", http.StatusBadRequest}
	}

	if req.PodName == nil {
		return &apiError{nil, "pod-name is missing", http.StatusBadRequest}
	}

	if req.CniNs == nil {
		return &apiError{nil, "cni-ns is missing", http.StatusBadRequest}
	}

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

	if req.CniNs == nil {
		return &apiError{nil, "cni-ns is missing", http.StatusBadRequest}
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
