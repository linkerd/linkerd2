package srv

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/gorilla/websocket"
	"github.com/julienschmidt/httprouter"
	"github.com/linkerd/linkerd2/controller/api/util"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	log "github.com/sirupsen/logrus"
)

type (
	jsonError struct {
		Error string `json:"error"`
	}
)

var (
	defaultResourceType = "deployments"
	pbMarshaler         = jsonpb.Marshaler{EmitDefaults: true}
	maxMessageSize      = 2048
	websocketUpgrader   = websocket.Upgrader{
		ReadBufferSize:  maxMessageSize,
		WriteBufferSize: maxMessageSize,
	}
)

func renderJsonError(w http.ResponseWriter, err error, status int) {
	w.Header().Set("Content-Type", "application/json")
	log.Error(err.Error())
	rsp, _ := json.Marshal(jsonError{Error: err.Error()})
	w.WriteHeader(status)
	w.Write(rsp)
}

func renderJson(w http.ResponseWriter, resp interface{}) {
	w.Header().Set("Content-Type", "application/json")
	jsonResp, err := json.Marshal(resp)
	if err != nil {
		renderJsonError(w, err, http.StatusInternalServerError)
		return
	}
	w.Write(jsonResp)
}

func renderJsonPb(w http.ResponseWriter, msg proto.Message) {
	w.Header().Set("Content-Type", "application/json")
	pbMarshaler.Marshal(w, msg)
}

func (h *handler) handleApiVersion(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	version, err := h.apiClient.Version(req.Context(), &pb.Empty{})

	if err != nil {
		renderJsonError(w, err, http.StatusInternalServerError)
		return
	}
	resp := map[string]interface{}{
		"version": version,
	}
	renderJson(w, resp)
}

func (h *handler) handleApiPods(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	pods, err := h.apiClient.ListPods(req.Context(), &pb.ListPodsRequest{
		Namespace: req.FormValue("namespace"),
	})

	if err != nil {
		renderJsonError(w, err, http.StatusInternalServerError)
		return
	}

	renderJsonPb(w, pods)
}

func (h *handler) handleApiStat(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	allNs := false
	if req.FormValue("all_namespaces") == "true" {
		allNs = true
	}
	requestParams := util.StatSummaryRequestParams{
		TimeWindow:    req.FormValue("window"),
		ResourceName:  req.FormValue("resource_name"),
		ResourceType:  req.FormValue("resource_type"),
		Namespace:     req.FormValue("namespace"),
		ToName:        req.FormValue("to_name"),
		ToType:        req.FormValue("to_type"),
		ToNamespace:   req.FormValue("to_namespace"),
		FromName:      req.FormValue("from_name"),
		FromType:      req.FormValue("from_type"),
		FromNamespace: req.FormValue("from_namespace"),
		AllNamespaces: allNs,
	}

	// default to returning deployment stats
	if requestParams.ResourceType == "" {
		requestParams.ResourceType = defaultResourceType
	}

	statRequest, err := util.BuildStatSummaryRequest(requestParams)
	if err != nil {
		renderJsonError(w, err, http.StatusInternalServerError)
		return
	}

	result, err := h.apiClient.StatSummary(req.Context(), statRequest)
	if err != nil {
		renderJsonError(w, err, http.StatusInternalServerError)
		return
	}
	renderJsonPb(w, result)
}

func (h *handler) handleApiTap(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	ws, err := websocketUpgrader.Upgrade(w, req, nil)
	if err != nil {
		renderJsonError(w, err, http.StatusInternalServerError)
		return
	}
	defer ws.Close()

	var requestParams util.TapRequestParams
	messageType, message, err := ws.ReadMessage()
	if messageType == websocket.TextMessage {
		err := json.Unmarshal(message, &requestParams)
		if err != nil {
			ws.WriteMessage(websocket.CloseMessage, []byte(err.Error()))
			return
		}
	}

	if requestParams.MaxRps == 0.0 {
		requestParams.MaxRps = 1.0
	}

	tapReq, err := util.BuildTapByResourceRequest(requestParams)
	if err != nil {
		ws.WriteMessage(websocket.CloseMessage, []byte(err.Error()))
		return
	}

	tapClient, err := h.apiClient.TapByResource(req.Context(), tapReq)
	if err != nil {
		ws.WriteMessage(websocket.CloseMessage, []byte(err.Error()))
		return
	}
	defer tapClient.CloseSend()

	go func() {
		for {
			rsp, err := tapClient.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				ws.WriteMessage(websocket.CloseMessage, []byte(err.Error()))
				break
			}

			tapEvent := util.RenderTapEvent(rsp)
			if err := ws.WriteMessage(websocket.TextMessage, []byte(tapEvent)); err != nil {
				log.Error(err)
				break
			}
		}
	}()

	for {
		messageType, _, err := ws.ReadMessage()
		if err != nil {
			log.Error(err)
			break
		}
		if messageType == websocket.CloseMessage {
			break
		}
	}
}
