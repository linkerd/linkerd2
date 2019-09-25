package api

type StartProxyRequest struct {
	PodIP        *string `json:"pod-ip"`
	PodSandboxID *string `json:"pod-sandbox-id"`
	PodName      *string `json:"pod-name"`
	PodNamespace *string `json:"pod-namespace"`
	CniNs        *string `json:"cni-ns"`
}

type StopProxyRequest struct {
	PodSandboxID *string `json:"pod-sandbox-id"`
}

type ReadinessCheckRequest struct {
	PodName *string `json:"pod-name"`
	PodNamespace *string `json:"pod-namespace"`
	PodIP *string `json:"pod-ip"`
	CniNs *string `json:"cni-ns"`
}
