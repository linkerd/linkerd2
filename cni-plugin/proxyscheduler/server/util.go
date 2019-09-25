package server

import (
	"encoding/json"
	"github.com/julienschmidt/httprouter"
	"github.com/sirupsen/logrus"
	"net/http"
)

const (
	schedulerManagedPodNsAnnotation = "l5d.proxyscheduler.pod.ns"
	schedulerManagedPodAnnotation   = "l5d.proxyscheduler.managed.pod"
	schedulerCreatedContainerLabel  = "l5d.schleduler.proxy"
	trueValue                       = "true"
	ReadinessCheckInitialDelayMs  = 10000
	LivenessCheckInitialDelayMs   = 2000
	LivenessCheckIntervalMs       = 5000
)


type (
	apiError struct {
		Error error `json:"error,omitempty"`
		Message string `json:"message"`
		Status int  `json:"status"`
	}
)

func handleApiError(handler func(w http.ResponseWriter, r *http.Request,  p httprouter.Params) *apiError) func(http.ResponseWriter, *http.Request,  httprouter.Params) {
	return func(w http.ResponseWriter, r *http.Request,  p httprouter.Params) {
		err := handler(w, r, p )
		if err != nil {
			logrus.Errorf("Internal error when handling request: %v", err)
			jsonErrorResponse(w, err)
		}
	}
}

func jsonErrorResponse(w http.ResponseWriter, err *apiError) {
	w.Header().Set("Content-Type", "application/json")
	rsp, _ := json.Marshal(err)
	w.WriteHeader(err.Status)
	w.Write(rsp)
}

func statusResponse(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
}


