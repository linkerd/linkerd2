package srv

import (
	"fmt"
	"html/template"
	"net/http"
	"path"
	"path/filepath"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/controller/util"
	"github.com/runconduit/conduit/web/util/filesonly"
	log "github.com/sirupsen/logrus"
)

const (
	timeout = 10 * time.Second
)

type (
	Server struct {
		templateDir     string
		staticDir       string
		reload          bool
		templateContext templateContext
		templates       map[string]*template.Template
		router          *httprouter.Router
	}

	templateContext struct {
		WebpackDevServer string
	}
	templatePayload struct {
		Context  templateContext
		Contents interface{}
	}
	appParams struct {
		Data         *pb.VersionInfo
		UUID         string
		Error        bool
		ErrorMessage string
	}
)

// this is called by the HTTP server to actually respond to a request
func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	s.router.ServeHTTP(w, req)
}

func NewServer(addr, templateDir, staticDir, uuid, webpackDevServer string, reload bool, apiClient pb.ApiClient) *http.Server {
	counter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "A counter for requests to the wrapped handler.",
		},
		[]string{"code"},
	)

	duration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "A histogram of latencies for requests in seconds.",
			Buckets: util.RequestDurationBucketsSeconds,
		},
		[]string{"code"},
	)

	responseSize := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_response_size_bytes",
			Help:    "A histogram of response sizes for requests.",
			Buckets: util.ResponseSizeBuckets,
		},
		[]string{},
	)

	prometheus.MustRegister(counter, duration, responseSize)

	server := &Server{
		templateDir:     templateDir,
		staticDir:       staticDir,
		templateContext: templateContext{webpackDevServer},
		reload:          reload,
	}
	server.router = &httprouter.Router{
		RedirectTrailingSlash:  true,
		RedirectFixedPath:      true,
		HandleMethodNotAllowed: false, // disable 405s
	}

	wrappedServer := promhttp.InstrumentHandlerDuration(duration,
		promhttp.InstrumentHandlerResponseSize(responseSize,
			promhttp.InstrumentHandlerCounter(counter, server)))

	handler := &handler{
		apiClient: apiClient,
		render:    server.RenderTemplate,
		serveFile: server.serveFile,
		uuid:      uuid,
	}

	httpServer := &http.Server{
		Addr:         addr,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
		Handler:      wrappedServer,
	}

	// webapp routes
	server.router.GET("/", handler.handleIndex)
	server.router.GET("/pod", handler.handleIndex)
	server.router.GET("/deployment", handler.handleIndex)
	server.router.GET("/deployments", handler.handleIndex)
	server.router.GET("/paths", handler.handleIndex)
	server.router.GET("/servicemesh", handler.handleIndex)
	server.router.GET("/routes", handler.handleIndex)
	server.router.ServeFiles(
		"/dist/*filepath", // add catch-all parameter to match all files in dir
		filesonly.FileSystem(server.staticDir))

	// webapp api routes
	server.router.GET("/api/version", handler.handleApiVersion)
	server.router.GET("/api/metrics", handler.handleApiMetrics)
	server.router.GET("/api/pods", handler.handleApiPods)

	return httpServer
}

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
	} else {
		return template.ExecuteTemplate(w, templateName, templatePayload{Context: s.templateContext, Contents: args})
	}
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

func safelyJoinPath(rootPath, userPath string) string {
	return filepath.Join(rootPath, path.Clean("/"+userPath))
}

func (s *Server) serveFile(w http.ResponseWriter, fileName string, templateName string, args interface{}) error {
	dispositionHeaderVal := fmt.Sprintf("attachment; filename='%s'", fileName)

	w.Header().Set("Content-Type", "text/yaml")
	w.Header().Set("Content-Disposition", dispositionHeaderVal)

	template, err := s.loadTemplate(templateName)
	if err != nil {
		return err
	}

	template.Execute(w, args)
	return nil
}
