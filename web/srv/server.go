package srv

import (
	"fmt"
	"html"
	"html/template"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/linkerd/linkerd2/pkg/filesonly"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	vizPb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/patrickmn/go-cache"
	log "github.com/sirupsen/logrus"
)

const (
	timeout = 10 * time.Second

	// statExpiration indicates when items in the stat cache expire.
	statExpiration = 1500 * time.Millisecond

	// statCleanupInterval indicates how often expired items in the stat cache
	// are cleaned up.
	statCleanupInterval = 5 * time.Minute
)

type (
	// Server encapsulates the Linkerd control plane's web dashboard server.
	Server struct {
		templateDir string
		reload      bool
		templates   map[string]*template.Template
		router      *httprouter.Router
		reHost      *regexp.Regexp
	}

	templatePayload struct {
		Contents interface{}
	}
	appParams struct {
		UUID                string
		ReleaseVersion      string
		ControllerNamespace string
		Error               bool
		ErrorMessage        string
		PathPrefix          string
		Jaeger              string
		Grafana             string
	}

	healthChecker interface {
		RunChecks(observer healthcheck.CheckObserver) bool
	}
)

// this is called by the HTTP server to actually respond to a request
func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if !s.reHost.MatchString(req.Host) {
		err := fmt.Sprintf(`It appears that you are trying to reach this service with a host of '%s'.
This does not match /%s/ and has been denied for security reasons.
Please see https://linkerd.io/dns-rebinding for an explanation of what is happening and how to fix it.`,
			html.EscapeString(req.Host),
			html.EscapeString(s.reHost.String()))
		http.Error(w, err, http.StatusBadRequest)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("X-XSS-Protection", "1; mode=block")
	s.router.ServeHTTP(w, req)
}

// NewServer returns an initialized `http.Server`, configured to listen on an
// address, render templates, and serve static assets, for a given Linkerd
// control plane.
func NewServer(
	addr string,
	grafanaAddr string,
	jaegerAddr string,
	templateDir string,
	staticDir string,
	uuid string,
	version string,
	controllerNamespace string,
	clusterDomain string,
	reload bool,
	reHost *regexp.Regexp,
	apiClient vizPb.ApiClient,
	k8sAPI *k8s.KubernetesAPI,
	hc healthChecker,
) *http.Server {
	server := &Server{
		templateDir: templateDir,
		reload:      reload,
		reHost:      reHost,
	}

	server.router = &httprouter.Router{
		RedirectTrailingSlash:  true,
		RedirectFixedPath:      true,
		HandleMethodNotAllowed: false, // disable 405s
	}

	wrappedServer := prometheus.WithTelemetry(server)
	handler := &handler{
		apiClient:           apiClient,
		k8sAPI:              k8sAPI,
		render:              server.RenderTemplate,
		uuid:                uuid,
		version:             version,
		controllerNamespace: controllerNamespace,
		clusterDomain:       clusterDomain,
		grafanaProxy:        newReverseProxy(grafanaAddr, "/grafana"),
		jaegerProxy:         newReverseProxy(jaegerAddr, ""),
		grafana:             grafanaAddr,
		jaeger:              jaegerAddr,
		hc:                  hc,
		statCache:           cache.New(statExpiration, statCleanupInterval),
	}

	httpServer := &http.Server{
		Addr:         addr,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
		Handler:      wrappedServer,
	}

	// webapp routes
	server.router.GET("/", handler.handleIndex)
	server.router.GET("/controlplane", handler.handleIndex)
	server.router.GET("/namespaces", handler.handleIndex)
	server.router.GET("/gateways", handler.handleIndex)

	// paths for a list of resources by namespace
	server.router.GET("/namespaces/:namespace/daemonsets", handler.handleIndex)
	server.router.GET("/namespaces/:namespace/statefulsets", handler.handleIndex)
	server.router.GET("/namespaces/:namespace/trafficsplits", handler.handleIndex)
	server.router.GET("/namespaces/:namespace/jobs", handler.handleIndex)
	server.router.GET("/namespaces/:namespace/deployments", handler.handleIndex)
	server.router.GET("/namespaces/:namespace/replicationcontrollers", handler.handleIndex)
	server.router.GET("/namespaces/:namespace/pods", handler.handleIndex)
	server.router.GET("/namespaces/:namespace/cronjobs", handler.handleIndex)
	server.router.GET("/namespaces/:namespace/replicasets", handler.handleIndex)

	// legacy paths that are deprecated but should not 404
	server.router.GET("/overview", handler.handleIndex)
	server.router.GET("/daemonsets", handler.handleIndex)
	server.router.GET("/statefulsets", handler.handleIndex)
	server.router.GET("/trafficsplits", handler.handleIndex)
	server.router.GET("/jobs", handler.handleIndex)
	server.router.GET("/deployments", handler.handleIndex)
	server.router.GET("/replicationcontrollers", handler.handleIndex)
	server.router.GET("/pods", handler.handleIndex)

	// paths for individual resource view
	server.router.GET("/namespaces/:namespace", handler.handleIndex)
	server.router.GET("/namespaces/:namespace/pods/:pod", handler.handleIndex)
	server.router.GET("/namespaces/:namespace/daemonsets/:daemonset", handler.handleIndex)
	server.router.GET("/namespaces/:namespace/statefulsets/:statefulset", handler.handleIndex)
	server.router.GET("/namespaces/:namespace/trafficsplits/:trafficsplit", handler.handleIndex)
	server.router.GET("/namespaces/:namespace/deployments/:deployment", handler.handleIndex)
	server.router.GET("/namespaces/:namespace/jobs/:job", handler.handleIndex)
	server.router.GET("/namespaces/:namespace/replicationcontrollers/:replicationcontroller", handler.handleIndex)
	server.router.GET("/namespaces/:namespace/cronjobs/:cronjob", handler.handleIndex)
	server.router.GET("/namespaces/:namespace/replicasets/:replicaset", handler.handleIndex)

	// tools and community paths
	server.router.GET("/tap", handler.handleIndex)
	server.router.GET("/top", handler.handleIndex)
	server.router.GET("/community", handler.handleIndex)
	server.router.GET("/routes", handler.handleIndex)
	server.router.GET("/profiles/new", handler.handleProfileDownload)

	// add catch-all parameter to match all files in dir
	server.router.GET("/dist/*filepath", mkStaticHandler(staticDir))

	// webapp api routes
	server.router.GET("/api/version", handler.handleAPIVersion)
	// Traffic Performance Summary.  This route used to be called /api/stat
	// but was renamed to avoid triggering ad blockers.
	// See: https://github.com/linkerd/linkerd2/issues/970
	server.router.GET("/api/tps-reports", handler.handleAPIStat)
	server.router.GET("/api/pods", handler.handleAPIPods)
	server.router.GET("/api/services", handler.handleAPIServices)
	server.router.GET("/api/tap", handler.handleAPITap)
	server.router.GET("/api/routes", handler.handleAPITopRoutes)
	server.router.GET("/api/edges", handler.handleAPIEdges)
	server.router.GET("/api/check", handler.handleAPICheck)
	server.router.GET("/api/resource-definition", handler.handleAPIResourceDefinition)
	server.router.GET("/api/gateways", handler.handleAPIGateways)
	server.router.GET("/api/extension", handler.handleGetExtension)

	// grafana proxy
	server.handleAllOperationsForPath("/grafana/*grafanapath", handler.handleGrafana)

	// jaeger proxy
	server.handleAllOperationsForPath("/jaeger/*jaegerpath", handler.handleJaeger)

	return httpServer
}

// RenderTemplate writes a rendered template into a buffer, given an HTTP
// request and template information.
func (s *Server) RenderTemplate(w http.ResponseWriter, templateFile, templateName string, args interface{}) error {
	log.Debugf("emitting template %s", templateFile)
	template, err := s.loadTemplate(templateFile)

	if err != nil {
		log.Error(err.Error())
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil
	}

	w.Header().Set("Content-Type", "text/html")
	if templateName == "" {
		return template.Execute(w, args)
	}

	return template.ExecuteTemplate(w, templateName, templatePayload{Contents: args})
}

func (s *Server) loadTemplate(templateFile string) (template *template.Template, err error) {
	// load template from disk if necessary
	template = s.templates[templateFile]

	if template == nil || s.reload {
		templatePath := safelyJoinPath(s.templateDir, templateFile)
		includes, err := filepath.Glob(filepath.Join(s.templateDir, "includes", "*.tmpl.html"))
		if err != nil {
			return nil, err
		}
		// for cases where you're not calling a named template, the passed-in path needs to be first
		templateFiles := append([]string{templatePath}, includes...)
		log.Debugf("loading templates from %v", templateFiles)
		template, err = template.ParseFiles(templateFiles...)
		if err == nil && !s.reload {
			s.templates[templateFile] = template
		}
	}
	return template, err
}

func (s *Server) handleAllOperationsForPath(path string, handle httprouter.Handle) {
	s.router.DELETE(path, handle)
	s.router.GET(path, handle)
	s.router.HEAD(path, handle)
	s.router.OPTIONS(path, handle)
	s.router.PATCH(path, handle)
	s.router.POST(path, handle)
	s.router.PUT(path, handle)
}

func safelyJoinPath(rootPath, userPath string) string {
	return filepath.Join(rootPath, path.Clean("/"+userPath))
}

func mkStaticHandler(staticDir string) httprouter.Handle {
	fileServer := http.FileServer(filesonly.FileSystem(staticDir))

	return func(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
		filepath := p.ByName("filepath")
		if filepath == "/index_bundle.js" {
			// don't cache the bundle because it references a hashed js file
			w.Header().Set("Cache-Control", "no-store, must-revalidate")
		}

		req.URL.Path = filepath
		fileServer.ServeHTTP(w, req)
	}
}
