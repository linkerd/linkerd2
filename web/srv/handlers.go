package srv

import (
	"net/http"

	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/julienschmidt/httprouter"
	log "github.com/sirupsen/logrus"
)

type (
	renderTemplate func(http.ResponseWriter, string, string, interface{}) error
	serveFile      func(http.ResponseWriter, string, string, interface{}) error

	handler struct {
		render    renderTemplate
		serveFile serveFile
		apiClient pb.ApiClient
		namespace string
		uuid      string
	}
)

func (h *handler) handleIndex(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	params := appParams{Namespace: h.namespace, UUID: h.uuid}

	version, err := h.apiClient.Version(req.Context(), &pb.Empty{}) // TODO: remove and call /api/version from web app
	if err != nil {
		params.Error = true
		params.ErrorMessage = err.Error()
		log.Error(err.Error())
	} else {
		params.Data = version
	}

	err = h.render(w, "app.tmpl.html", "base", params)

	if err != nil {
		log.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
	}
}
