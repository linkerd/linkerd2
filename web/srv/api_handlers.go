package srv

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/gorilla/websocket"
	"github.com/julienschmidt/httprouter"
	"github.com/linkerd/linkerd2/controller/api/util"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
)

type (
	jsonError struct {
		Error string `json:"error"`
	}
)

var (
	defaultResourceType = k8s.Deployment
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

func websocketError(ws *websocket.Conn, wsError int, msg string) {
	log.Infof("sending websocket close %d: %s", wsError, msg)
	ws.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(wsError, msg),
		time.Time{})
}

func (h *handler) handleApiTap(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	ws, err := websocketUpgrader.Upgrade(w, req, nil)
	if err != nil {
		renderJsonError(w, err, http.StatusInternalServerError)
		return
	}
	defer func() {
		log.Info("closing websocket connection")
		ws.Close()
	}()

	messageType, message, err := ws.ReadMessage()
	if err != nil {
		websocketError(ws, websocket.CloseInternalServerErr, err.Error())
		return
	}

	if messageType != websocket.TextMessage {
		websocketError(ws, websocket.CloseUnsupportedData, "MessageType not supported")
		return
	}

	var requestParams util.TapRequestParams
	err = json.Unmarshal(message, &requestParams)
	if err != nil {
		websocketError(ws, websocket.CloseInternalServerErr, err.Error())
		return
	}

	tapReq, err := util.BuildTapByResourceRequest(requestParams)
	if err != nil {
		websocketError(ws, websocket.CloseInternalServerErr, err.Error())
		return
	}

	tapClient, err := h.apiClient.TapByResource(req.Context(), tapReq)
	if err != nil {
		websocketError(ws, websocket.CloseInternalServerErr, err.Error())
		return
	}
	defer tapClient.CloseSend()

	go func() {
		for {
			rsp, err := tapClient.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				log.Errorf("Receive error: %s", err)
				return
			}

			buf := new(bytes.Buffer)
			err = pbMarshaler.Marshal(buf, rsp)
			if err != nil {
				websocketError(ws, websocket.CloseInternalServerErr, err.Error())
				return
			}

			select {
			case <-req.Context().Done():
				log.Infof("HTTP request context closed")
				return
			default:
				if err := ws.WriteMessage(websocket.TextMessage, []byte(buf.String())); err != nil {
					if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure) {
						log.Errorf("Unexpected close error, write side: %s", err)
					}
					break
				}
			}
		}
	}()

	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure) {
				log.Errorf("Unexpected close error, read side: %s", err)
			}
			break
		}
	}

	websocketError(ws, websocket.CloseNormalClosure, "")
}
