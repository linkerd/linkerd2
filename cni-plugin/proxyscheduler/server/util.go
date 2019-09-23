package server

import (
	"encoding/json"
	"github.com/julienschmidt/httprouter"
	"github.com/sirupsen/logrus"
	"net/http"
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

/*func jsonResponse(w http.ResponseWriter, resp interface{}) {
	w.Header().Set("Content-Type", "application/json")
	jsonResp, err := json.Marshal(resp)
	if err != nil {
		jsonErrorResponse(w, err, http.StatusInternalServerError)
		return
	}
	w.Write(jsonResp)
}*/

func statusResponse(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
}


