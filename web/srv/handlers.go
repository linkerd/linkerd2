package srv

import (
	"net/http"
	"regexp"

	"github.com/julienschmidt/httprouter"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	log "github.com/sirupsen/logrus"
)

var proxyPathRegexp = regexp.MustCompile("/api/v1/namespaces/.*/proxy/")

type (
	renderTemplate func(http.ResponseWriter, string, string, interface{}) error
	serveFile      func(http.ResponseWriter, string, string, interface{}) error

	handler struct {
		render              renderTemplate
		serveFile           serveFile
		apiClient           pb.ApiClient
		uuid                string
		controllerNamespace string
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
		PathPrefix:          pathPfx,
	}

	version, err := h.apiClient.Version(req.Context(), &pb.Empty{}) // TODO: remove and call /api/version from web app
	if err != nil {
		params.Error = true
		params.ErrorMessage = err.Error()
		log.Error(err.Error())
	} else {
		params.Data = *version
	}

	err = h.render(w, "app.tmpl.html", "base", params)

	if err != nil {
		log.Error(err.Error())
	}
}
