package srv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/gorilla/websocket"
	"github.com/julienschmidt/httprouter"
	"github.com/linkerd/linkerd2/controller/api/util"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	"github.com/linkerd/linkerd2/pkg/tap"
	log "github.com/sirupsen/logrus"
)

// Control Frame payload size can be no bigger than 125 bytes. 2 bytes are
// reserved for the status code when formatting the message.
const maxControlFrameMsgSize = 123

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

	// Checks whose description matches the following regexp won't be included
	// in the handleApiCheck output. In the context of the dashboard, some
	// checks like cli or kubectl versions ones may not be relevant.
	//
	// TODO(tegioz): use more reliable way to identify the checks that should
	// not be displayed in the dashboard (hint anchor is not unique).
	excludedChecksRE = regexp.MustCompile(`(?i)cli|(?i)kubectl`)
)

func renderJSONError(w http.ResponseWriter, err error, status int) {
	w.Header().Set("Content-Type", "application/json")
	log.Error(err.Error())
	rsp, _ := json.Marshal(jsonError{Error: err.Error()})
	w.WriteHeader(status)
	w.Write(rsp)
}

func renderJSON(w http.ResponseWriter, resp interface{}) {
	w.Header().Set("Content-Type", "application/json")
	jsonResp, err := json.Marshal(resp)
	if err != nil {
		renderJSONError(w, err, http.StatusInternalServerError)
		return
	}
	w.Write(jsonResp)
}

func renderJSONPb(w http.ResponseWriter, msg proto.Message) {
	w.Header().Set("Content-Type", "application/json")
	pbMarshaler.Marshal(w, msg)
}

func (h *handler) handleAPIVersion(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	version, err := h.apiClient.Version(req.Context(), &pb.Empty{})

	if err != nil {
		renderJSONError(w, err, http.StatusInternalServerError)
		return
	}
	resp := map[string]interface{}{
		"version": version,
	}
	renderJSON(w, resp)
}

func (h *handler) handleAPIPods(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	pods, err := h.apiClient.ListPods(req.Context(), &pb.ListPodsRequest{
		Selector: &pb.ResourceSelection{
			Resource: &pb.Resource{
				Namespace: req.FormValue("namespace"),
			},
		},
	})

	if err != nil {
		renderJSONError(w, err, http.StatusInternalServerError)
		return
	}

	renderJSONPb(w, pods)
}

func (h *handler) handleAPIServices(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	services, err := h.apiClient.ListServices(req.Context(), &pb.ListServicesRequest{
		Namespace: req.FormValue("namespace"),
	})

	if err != nil {
		renderJSONError(w, err, http.StatusInternalServerError)
		return
	}

	renderJSONPb(w, services)
}

func (h *handler) handleAPIStat(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	trueStr := fmt.Sprintf("%t", true)

	requestParams := util.StatsSummaryRequestParams{
		StatsBaseRequestParams: util.StatsBaseRequestParams{
			TimeWindow:    req.FormValue("window"),
			ResourceName:  req.FormValue("resource_name"),
			ResourceType:  req.FormValue("resource_type"),
			Namespace:     req.FormValue("namespace"),
			AllNamespaces: req.FormValue("all_namespaces") == trueStr,
		},
		ToName:        req.FormValue("to_name"),
		ToType:        req.FormValue("to_type"),
		ToNamespace:   req.FormValue("to_namespace"),
		FromName:      req.FormValue("from_name"),
		FromType:      req.FormValue("from_type"),
		FromNamespace: req.FormValue("from_namespace"),
		SkipStats:     req.FormValue("skip_stats") == trueStr,
		TCPStats:      req.FormValue("tcp_stats") == trueStr,
	}

	// default to returning deployment stats
	if requestParams.ResourceType == "" {
		requestParams.ResourceType = defaultResourceType
	}

	statRequest, err := util.BuildStatSummaryRequest(requestParams)
	if err != nil {
		renderJSONError(w, err, http.StatusInternalServerError)
		return
	}

	result, err := h.apiClient.StatSummary(req.Context(), statRequest)
	if err != nil {
		renderJSONError(w, err, http.StatusInternalServerError)
		return
	}
	renderJSONPb(w, result)
}

func (h *handler) handleAPITopRoutes(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	requestParams := util.TopRoutesRequestParams{
		StatsBaseRequestParams: util.StatsBaseRequestParams{
			TimeWindow:   req.FormValue("window"),
			ResourceName: req.FormValue("resource_name"),
			ResourceType: req.FormValue("resource_type"),
			Namespace:    req.FormValue("namespace"),
		},
		ToName:      req.FormValue("to_name"),
		ToType:      req.FormValue("to_type"),
		ToNamespace: req.FormValue("to_namespace"),
	}

	topReq, err := util.BuildTopRoutesRequest(requestParams)
	if err != nil {
		renderJSONError(w, err, http.StatusBadRequest)
		return
	}

	result, err := h.apiClient.TopRoutes(req.Context(), topReq)
	if err != nil {
		renderJSONError(w, err, http.StatusInternalServerError)
		return
	}

	renderJSONPb(w, result)
}

// Control frame payload size must be no longer than `maxControlFrameMsgSize`
// bytes. In the case of an unexpected HTTP status code or unexpected error,
// truncate the message after `maxControlFrameMsgSize` bytes so that the web
// socket message is properly written.
func validateControlFrameMsg(err error) string {
	log.Debugf("tap error: %s", err.Error())

	msg := err.Error()
	if len(msg) > maxControlFrameMsgSize {
		return msg[:maxControlFrameMsgSize]
	}

	return msg
}

func websocketError(ws *websocket.Conn, wsError int, err error) {
	msg := validateControlFrameMsg(err)

	err = ws.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(wsError, msg),
		time.Time{})
	if err != nil {
		log.Errorf("Unexpected websocket error: %s", err)
	}
}

func (h *handler) handleAPITap(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	ws, err := websocketUpgrader.Upgrade(w, req, nil)
	if err != nil {
		renderJSONError(w, err, http.StatusInternalServerError)
		return
	}
	defer ws.Close()

	messageType, message, err := ws.ReadMessage()
	if err != nil {
		websocketError(ws, websocket.CloseInternalServerErr, err)
		return
	}

	if messageType != websocket.TextMessage {
		websocketError(ws, websocket.CloseUnsupportedData, errors.New("messageType not supported"))
		return
	}

	var requestParams util.TapRequestParams
	err = json.Unmarshal(message, &requestParams)
	if err != nil {
		websocketError(ws, websocket.CloseInternalServerErr, err)
		return
	}

	tapReq, err := util.BuildTapByResourceRequest(requestParams)
	if err != nil {
		websocketError(ws, websocket.CloseInternalServerErr, err)
		return
	}

	go func() {
		reader, body, err := tap.Reader(h.k8sAPI, tapReq, 0)
		if err != nil {
			// If there was a [403] error when initiating a tap, close the
			// socket with `ClosePolicyViolation` status code so that the error
			// renders without the error prefix in the banner
			if httpErr, ok := err.(protohttp.HTTPError); ok && httpErr.Code == http.StatusForbidden {
				err := fmt.Errorf("missing authorization, visit %s to remedy", tap.TapRbacURL)
				websocketError(ws, websocket.ClosePolicyViolation, err)
				return
			}

			// All other errors from initiating a tap should close with
			// `CloseInternalServerErr` status code
			websocketError(ws, websocket.CloseInternalServerErr, err)
			return
		}
		defer body.Close()

		for {
			event := pb.TapEvent{}
			err := protohttp.FromByteStreamToProtocolBuffers(reader, &event)
			if err == io.EOF {
				break
			}
			if err != nil {
				websocketError(ws, websocket.CloseInternalServerErr, err)
				break
			}

			buf := new(bytes.Buffer)
			err = pbMarshaler.Marshal(buf, &event)
			if err != nil {
				websocketError(ws, websocket.CloseInternalServerErr, err)
				break
			}

			if err := ws.WriteMessage(websocket.TextMessage, buf.Bytes()); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure) {
					log.Error(err)
				}
				break
			}
		}
	}()

	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			log.Debugf("Received close frame: %v", err)
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure) {
				log.Errorf("Unexpected close error: %s", err)
			}
			return
		}
	}
}

func (h *handler) handleAPIEdges(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	requestParams := util.EdgesRequestParams{
		Namespace:    req.FormValue("namespace"),
		ResourceType: req.FormValue("resource_type"),
	}

	edgesRequest, err := util.BuildEdgesRequest(requestParams)
	if err != nil {
		renderJSONError(w, err, http.StatusInternalServerError)
		return
	}

	result, err := h.apiClient.Edges(req.Context(), edgesRequest)
	if err != nil {
		renderJSONError(w, err, http.StatusInternalServerError)
		return
	}
	renderJSONPb(w, result)
}

func (h *handler) handleAPICheck(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	type CheckResult struct {
		*healthcheck.CheckResult
		ErrMsg  string `json:",omitempty"`
		HintURL string `json:",omitempty"`
	}

	results := make(map[healthcheck.CategoryID][]*CheckResult)

	collectResults := func(result *healthcheck.CheckResult) {
		if result.Retry || excludedChecksRE.MatchString(result.Description) {
			return
		}
		var errMsg, hintURL string
		if result.Err != nil {
			errMsg = result.Err.Error()
			hintURL = fmt.Sprintf("%s%s", healthcheck.HintBaseURL, result.HintAnchor)
		}
		results[result.Category] = append(results[result.Category], &CheckResult{
			CheckResult: result,
			ErrMsg:      errMsg,
			HintURL:     hintURL,
		})
	}
	success := h.hc.RunChecks(collectResults)

	renderJSON(w, map[string]interface{}{
		"success": success,
		"results": results,
	})
}
