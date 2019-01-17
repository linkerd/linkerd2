package srv

import (
	"bytes"
	"fmt"
	"net/http"
	"regexp"

	"github.com/julienschmidt/httprouter"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	profiles "github.com/linkerd/linkerd2/pkg/profiles"
	log "github.com/sirupsen/logrus"
)

var proxyPathRegexp = regexp.MustCompile("/api/v1/namespaces/.*/proxy/")

type (
	renderTemplate func(http.ResponseWriter, string, string, interface{}) error

	handler struct {
		render              renderTemplate
		apiClient           pb.ApiClient
		uuid                string
		controllerNamespace string
		singleNamespace     bool
		grafanaProxy        *grafanaProxy
	}
)

func (h *handler) handleIndex(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	// when running the dashboard via `linkerd dashboard`, serve the index bundle at the right path
	pathPfx := proxyPathRegexp.FindString(req.URL.Path)
	if pathPfx == "" {
		pathPfx = "/"
	}

	params := appParams{
		UUID:                h.uuid,
		ControllerNamespace: h.controllerNamespace,
		SingleNamespace:     h.singleNamespace,
		PathPrefix:          pathPfx,
	}

	version, err := h.apiClient.Version(req.Context(), &pb.Empty{}) // TODO: remove and call /api/version from web app
	if err != nil {
		params.Error = true
		params.ErrorMessage = err.Error()
		log.Error(err)
	} else {
		params.Data = *version
	}

	err = h.render(w, "app.tmpl.html", "base", params)

	if err != nil {
		log.Error(err)
	}
}

func (h *handler) handleProfileDownload(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	service := req.FormValue("service")
	namespace := req.FormValue("namespace")

	if service == "" || namespace == "" {
		err := fmt.Errorf("Service and namespace must be provided to create a new profile")
		log.Error(err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	profileYaml := &bytes.Buffer{}
	err := profiles.RenderProfileTemplate(namespace, service, h.controllerNamespace, profileYaml)

	if err != nil {
		log.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	dispositionHeaderVal := fmt.Sprintf("attachment; filename='%s-profile.yml'", service)

	w.Header().Set("Content-Type", "text/yaml")
	w.Header().Set("Content-Disposition", dispositionHeaderVal)

	w.Write(profileYaml.Bytes())
}

func (h *handler) handleGrafana(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	h.grafanaProxy.ServeHTTP(w, req)
}
