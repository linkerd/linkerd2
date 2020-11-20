package charts

import (
	"bytes"
	"net/http"
	"path"
	"strings"

	"github.com/linkerd/linkerd2/pkg/charts/static"
	"github.com/linkerd/linkerd2/pkg/version"
	"k8s.io/helm/pkg/chartutil"
	helmChart "k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/renderutil"
	"k8s.io/helm/pkg/timeconv"
)

const versionPlaceholder = "linkerdVersionValue"

// Chart holds the necessary info to render a Helm chart
type Chart struct {
	Name      string
	Namespace string
	RawValues []byte
	Files     []*chartutil.BufferedFile
	Fs        http.FileSystem
}

func (c *Chart) render(partialsFiles []*chartutil.BufferedFile) (bytes.Buffer, error) {
	if err := FilesReader(c.Fs, "", c.Files); err != nil {
		return bytes.Buffer{}, err
	}

	// partials are present only in the static.Templates FileSystem
	if err := FilesReader(static.WithDefaultChart("partials"), "", partialsFiles); err != nil {
		return bytes.Buffer{}, err
	}

	// Create chart and render templates
	chart, err := chartutil.LoadFiles(append(c.Files, partialsFiles...))
	if err != nil {
		return bytes.Buffer{}, err
	}

	renderOpts := renderutil.Options{
		ReleaseOptions: chartutil.ReleaseOptions{
			Name:      c.Name,
			IsInstall: true,
			IsUpgrade: false,
			Time:      timeconv.Now(),
			Namespace: c.Namespace,
		},
		KubeVersion: "",
	}

	chartConfig := &helmChart.Config{Raw: string(c.RawValues), Values: map[string]*helmChart.Value{}}
	renderedTemplates, err := renderutil.Render(chart, chartConfig, renderOpts)
	if err != nil {
		return bytes.Buffer{}, err
	}

	// Merge templates and inject
	var buf bytes.Buffer
	for _, tmpl := range c.Files {
		t := path.Join(c.Name, tmpl.Name)
		if _, err := buf.WriteString(renderedTemplates[t]); err != nil {
			return bytes.Buffer{}, err
		}
	}

	return buf, nil
}

// Render returns a bytes buffer with the result of rendering a Helm chart
func (c *Chart) Render() (bytes.Buffer, error) {

	// Keep this slice synced with the contents of /charts/partials
	l5dPartials := []*chartutil.BufferedFile{
		{Name: "templates/_proxy.tpl"},
		{Name: "templates/_proxy-init.tpl"},
		{Name: "templates/_volumes.tpl"},
		{Name: "templates/_resources.tpl"},
		{Name: "templates/_metadata.tpl"},
		{Name: "templates/_helpers.tpl"},
		{Name: "templates/_debug.tpl"},
		{Name: "templates/_capabilities.tpl"},
		{Name: "templates/_trace.tpl"},
		{Name: "templates/_nodeselector.tpl"},
		{Name: "templates/_tolerations.tpl"},
		{Name: "templates/_affinity.tpl"},
		{Name: "templates/_addons.tpl"},
		{Name: "templates/_validate.tpl"},
		{Name: "templates/_pull-secrets.tpl"},
	}
	return c.render(l5dPartials)
}

// RenderCNI returns a bytes buffer with the result of rendering a Helm chart
func (c *Chart) RenderCNI() (bytes.Buffer, error) {
	cniPartials := []*chartutil.BufferedFile{
		{Name: "templates/_helpers.tpl"},
		{Name: "templates/_pull-secrets.tpl"},
	}
	return c.render(cniPartials)
}

// RenderNoPartials returns a bytes buffer with the result of rendering a Helm chart with no partials
func (c *Chart) RenderNoPartials() (bytes.Buffer, error) {
	return c.render([]*chartutil.BufferedFile{})
}

// ReadFile updates the buffered file with the data read from disk
func ReadFile(fs http.FileSystem, dir string, f *chartutil.BufferedFile) error {
	filename := dir + f.Name
	file, err := fs.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(file); err != nil {
		return err
	}

	f.Data = buf.Bytes()
	return nil
}

// FilesReader reads all the files from a directory
func FilesReader(fs http.FileSystem, dir string, files []*chartutil.BufferedFile) error {
	for _, f := range files {
		if err := ReadFile(fs, dir, f); err != nil {
			return err
		}
	}
	return nil
}

// InsertVersion returns the chart values file contents passed in
// with the version placeholder replaced with the current version
func InsertVersion(data []byte) []byte {
	dataWithVersion := strings.Replace(string(data), versionPlaceholder, version.Version, 1)
	return []byte(dataWithVersion)
}
