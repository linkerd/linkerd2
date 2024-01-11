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
	timeout = 15 * time.Second

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
		basePath    string
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
		GrafanaExternalURL  string
		GrafanaPrefix       string
	}

	healthChecker interface {
		RunChecks(observer healthcheck.CheckObserver) (bool, bool)
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
	basePath string,
	grafanaAddr string,
	grafanaExternalAddr string,
	grafanaPrefix string,
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
		basePath:    basePath,
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
		jaegerProxy:         newReverseProxy(jaegerAddr, ""),
		grafana:             grafanaAddr,
		grafanaExternalURL:  grafanaExternalAddr,
		grafanaPrefix:       grafanaPrefix,
		jaeger:              jaegerAddr,
		hc:                  hc,
		statCache:           cache.New(statExpiration, statCleanupInterval),
		basePath:            basePath,
	}

	// Only create the grafana reverse proxy if we aren't using external grafana
	if grafanaExternalAddr == "" {
		handler.grafanaProxy = newReverseProxy(grafanaAddr, server.calculateAbsolutePath("/grafana"))
	}

	httpServer := &http.Server{
		Addr:              addr,
		ReadTimeout:       timeout,
		ReadHeaderTimeout: timeout,
		WriteTimeout:      timeout,
		Handler:           wrappedServer,
	}

	// webapp routes
	server.router.GET(server.calculateAbsolutePath("/"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/controlplane"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/gateways"), handler.handleIndex)

	// paths for a list of resources by namespace
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/daemonsets"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/statefulsets"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/jobs"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/deployments"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/services"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/replicationcontrollers"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/pods"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/cronjobs"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/replicasets"), handler.handleIndex)

	// legacy paths that are deprecated but should not 404
	server.router.GET(server.calculateAbsolutePath("/overview"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/daemonsets"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/statefulsets"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/jobs"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/deployments"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/services"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/replicationcontrollers"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/pods"), handler.handleIndex)

	// paths for individual resource view
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/pods/:pod"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/daemonsets/:daemonset"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/statefulsets/:statefulset"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/deployments/:deployment"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/services/:deployment"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/jobs/:job"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/replicationcontrollers/:replicationcontroller"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/cronjobs/:cronjob"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/namespaces/:namespace/replicasets/:replicaset"), handler.handleIndex)

	// tools and community paths
	server.router.GET(server.calculateAbsolutePath("/tap"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/top"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/community"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/routes"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/extensions"), handler.handleIndex)
	server.router.GET(server.calculateAbsolutePath("/profiles/new"), handler.handleProfileDownload)

	// add catch-all parameter to match all files in dir
	server.router.GET(server.calculateAbsolutePath("/dist/*filepath"), mkStaticHandler(staticDir))

	// webapp api routes
	server.router.GET(server.calculateAbsolutePath("/api/version"), handler.handleAPIVersion)
	// Traffic Performance Summary.  This route used to be called /api/stat
	// but was renamed to avoid triggering ad blockers.
	// See: https://github.com/linkerd/linkerd2/issues/970
	server.router.GET(server.calculateAbsolutePath("/api/tps-reports"), handler.handleAPIStat)
	server.router.GET(server.calculateAbsolutePath("/api/pods"), handler.handleAPIPods)
	server.router.GET(server.calculateAbsolutePath("/api/services"), handler.handleAPIServices)
	server.router.GET(server.calculateAbsolutePath("/api/tap"), handler.handleAPITap)
	server.router.GET(server.calculateAbsolutePath("/api/routes"), handler.handleAPITopRoutes)
	server.router.GET(server.calculateAbsolutePath("/api/edges"), handler.handleAPIEdges)
	server.router.GET(server.calculateAbsolutePath("/api/check"), handler.handleAPICheck)
	server.router.GET(server.calculateAbsolutePath("/api/resource-definition"), handler.handleAPIResourceDefinition)
	server.router.GET(server.calculateAbsolutePath("/api/gateways"), handler.handleAPIGateways)
	server.router.GET(server.calculateAbsolutePath("/api/extensions"), handler.handleGetExtensions)

	// grafana proxy, only used if external grafana is not in use
	if grafanaExternalAddr == "" {
		server.handleAllOperationsForPath(server.calculateAbsolutePath("/grafana/*grafanapath"), handler.handleGrafana)
	}

	// jaeger proxy
	server.handleAllOperationsForPath(server.calculateAbsolutePath("/jaeger/*jaegerpath"), handler.handleJaeger)

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

func (s *Server) calculateAbsolutePath(relPath string) string {
	if relPath == "" {
		return s.basePath
	}

	return path.Join(s.basePath, relPath)
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
