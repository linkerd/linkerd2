package api

type StartProxyRequest struct {
	PodIP        *string `json:"pod-ip"`
	PodSandboxID *string `json:"pod-sandbox-id"`
	PodName      *string `json:"pod-name"`
	PodNamespace *string `json:"pod-namespace"`
}

type StopProxyRequest struct {
	PodSandboxID *string `json:"pod-sandbox-id"`
}


