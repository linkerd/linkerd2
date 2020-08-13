package charts

import (
	"bytes"
	"path"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/pkg/charts/static"
	"github.com/linkerd/linkerd2/pkg/version"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
)

const versionPlaceholder = "{version}"

// Chart holds the necessary info to render a Helm chart
type Chart struct {
	Name      string
	Dir       string
	Namespace string
	RawValues []byte
	Files     []*loader.BufferedFile
}

func (c *Chart) render(partialsFiles []*loader.BufferedFile) (bytes.Buffer, error) {
	if err := FilesReader(c.Dir+"/", c.Files); err != nil {
		return bytes.Buffer{}, err
	}

	if err := FilesReader("", partialsFiles); err != nil {
		return bytes.Buffer{}, err
	}

	// Create chart and render templates
	chart, err := loader.LoadFiles(append(c.Files, partialsFiles...))
	if err != nil {
		return bytes.Buffer{}, err
	}

	releaseOptions := chartutil.ReleaseOptions{
		Name:      c.Name,
		IsInstall: true,
		IsUpgrade: false,
		Namespace: c.Namespace,
	}

	var rawMapValues map[string]interface{}
	err = yaml.Unmarshal(c.RawValues, &rawMapValues)
	if err != nil {
		return bytes.Buffer{}, err
	}

	valuesToRender, err := chartutil.ToRenderValues(chart, rawMapValues, releaseOptions, nil)
	if err != nil {
		return bytes.Buffer{}, err
	}

	renderedTemplates, err := engine.Render(chart, valuesToRender)
	if err != nil {
		return bytes.Buffer{}, err
	}

	// Merge templates and inject
	var buf bytes.Buffer
	for _, tmpl := range c.Files {
		t := path.Join(releaseOptions.Name, tmpl.Name)
		if _, err := buf.WriteString(renderedTemplates[t]); err != nil {
			return bytes.Buffer{}, err
		}
	}

	return buf, nil
}

// Render returns a bytes buffer with the result of rendering a Helm chart
func (c *Chart) Render() (bytes.Buffer, error) {

	// Keep this slice synced with the contents of /charts/partials
	l5dPartials := []*loader.BufferedFile{
		{Name: "charts/partials/" + chartutil.ChartfileName},
		{Name: "charts/partials/templates/_proxy.tpl"},
		{Name: "charts/partials/templates/_proxy-init.tpl"},
		{Name: "charts/partials/templates/_volumes.tpl"},
		{Name: "charts/partials/templates/_resources.tpl"},
		{Name: "charts/partials/templates/_metadata.tpl"},
		{Name: "charts/partials/templates/_helpers.tpl"},
		{Name: "charts/partials/templates/_debug.tpl"},
		{Name: "charts/partials/templates/_capabilities.tpl"},
		{Name: "charts/partials/templates/_trace.tpl"},
		{Name: "charts/partials/templates/_nodeselector.tpl"},
		{Name: "charts/partials/templates/_tolerations.tpl"},
		{Name: "charts/partials/templates/_affinity.tpl"},
		{Name: "charts/partials/templates/_addons.tpl"},
		{Name: "charts/partials/templates/_validate.tpl"},
	}
	return c.render(l5dPartials)
}

// RenderCNI returns a bytes buffer with the result of rendering a Helm chart
func (c *Chart) RenderCNI() (bytes.Buffer, error) {
	cniPartials := []*loader.BufferedFile{
		{Name: "charts/partials/" + chartutil.ChartfileName},
		{Name: "charts/partials/templates/_helpers.tpl"},
	}
	return c.render(cniPartials)
}

// RenderNoPartials returns a bytes buffer with the result of rendering a Helm chart with no partials
func (c *Chart) RenderNoPartials() (bytes.Buffer, error) {
	return c.render([]*loader.BufferedFile{})
}

// ReadFile updates the buffered file with the data read from disk
func ReadFile(dir string, f *loader.BufferedFile) error {
	filename := dir + f.Name
	if dir == "" {
		filename = filename[7:]
	}
	file, err := static.Templates.Open(filename)
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
func FilesReader(dir string, files []*loader.BufferedFile) error {
	for _, f := range files {
		if err := ReadFile(dir, f); err != nil {
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
