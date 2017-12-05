package srv

import (
	"encoding/json"
	"net/http"

	"github.com/runconduit/conduit/controller/api/util"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/julienschmidt/httprouter"
	log "github.com/sirupsen/logrus"
)

type (
	jsonError struct {
		Error string `json:"error"`
	}
)

var (
	defaultMetricTimeWindow      = pb.TimeWindow_ONE_MIN
	defaultMetricAggregationType = pb.AggregationType_TARGET_DEPLOY

	allMetrics = []pb.MetricName{
		pb.MetricName_REQUEST_RATE,
		pb.MetricName_SUCCESS_RATE,
		pb.MetricName_LATENCY,
	}

	meshMetrics = []pb.MetricName{
		pb.MetricName_REQUEST_RATE,
		pb.MetricName_SUCCESS_RATE,
	}

	pbMarshaler = jsonpb.Marshaler{EmitDefaults: true}
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

func validateMetricParams(metricNameParam, aggParam, timeWindowParam string) (
	metrics []pb.MetricName,
	groupBy pb.AggregationType,
	window pb.TimeWindow,
	err error,
) {
	groupBy = defaultMetricAggregationType
	if aggParam != "" {
		groupBy, err = util.GetAggregationType(aggParam)
		if err != nil {
			return
		}
	}

	metrics = allMetrics
	if metricNameParam != "" {
		var requestedMetricName pb.MetricName
		requestedMetricName, err = util.GetMetricName(metricNameParam)
		if err != nil {
			return
		}
		metrics = []pb.MetricName{requestedMetricName}
	} else if groupBy == pb.AggregationType_MESH {
		metrics = meshMetrics
	}

	window = defaultMetricTimeWindow
	if timeWindowParam != "" {
		var requestedWindow pb.TimeWindow
		requestedWindow, err = util.GetWindow(timeWindowParam)
		if err != nil {
			return
		}
		window = requestedWindow
	}

	return
}

func (h *handler) handleApiMetrics(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	metricNameParam := req.FormValue("metric")
	timeWindowParam := req.FormValue("window")
	aggParam := req.FormValue("aggregation")
	timeseries := req.FormValue("timeseries") == "true"

	filterBy := pb.MetricMetadata{
		TargetPod:    req.FormValue("target_pod"),
		TargetDeploy: req.FormValue("target_deploy"),
		SourcePod:    req.FormValue("source_pod"),
		SourceDeploy: req.FormValue("source_deploy"),
		Component:    req.FormValue("component"),
	}

	metrics, groupBy, window, err := validateMetricParams(metricNameParam, aggParam, timeWindowParam)
	if err != nil {
		renderJsonError(w, err, http.StatusBadRequest)
		return
	}

	metricsRequest := &pb.MetricRequest{
		Metrics:   metrics,
		Window:    window,
		FilterBy:  &filterBy,
		GroupBy:   groupBy,
		Summarize: !timeseries,
	}

	metricsResponse, err := h.apiClient.Stat(req.Context(), metricsRequest)
	if err != nil {
		renderJsonError(w, err, http.StatusInternalServerError)
		return
	}

	renderJsonPb(w, metricsResponse)
}

func (h *handler) handleApiPods(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	pods, err := h.apiClient.ListPods(req.Context(), &pb.Empty{})
	if err != nil {
		renderJsonError(w, err, http.StatusInternalServerError)
		return
	}

	renderJsonPb(w, pods)
}
