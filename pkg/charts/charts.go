package charts

import (
	"bytes"
	"net/http"
	"path"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/pkg/charts/static"
	"github.com/linkerd/linkerd2/pkg/version"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
)

const versionPlaceholder = "linkerdVersionValue"

var (
	// L5dPartials is the list of templates in partials chart
	// Keep this slice synced with the contents of /charts/partials
	L5dPartials = []string{
		"charts/partials/" + chartutil.ChartfileName,
		"charts/partials/templates/_proxy.tpl",
		"charts/partials/templates/_proxy-config-ann.tpl",
		"charts/partials/templates/_proxy-init.tpl",
		"charts/partials/templates/_volumes.tpl",
		"charts/partials/templates/_resources.tpl",
		"charts/partials/templates/_metadata.tpl",
		"charts/partials/templates/_helpers.tpl",
		"charts/partials/templates/_debug.tpl",
		"charts/partials/templates/_capabilities.tpl",
		"charts/partials/templates/_trace.tpl",
		"charts/partials/templates/_nodeselector.tpl",
		"charts/partials/templates/_tolerations.tpl",
		"charts/partials/templates/_affinity.tpl",
		"charts/partials/templates/_validate.tpl",
		"charts/partials/templates/_pull-secrets.tpl",
	}
)

// Chart holds the necessary info to render a Helm chart
type Chart struct {
	Name      string
	Dir       string
	Namespace string
	RawValues []byte
	Files     []*loader.BufferedFile
	Fs        http.FileSystem
}

func (c *Chart) render(partialsFiles []*loader.BufferedFile) (bytes.Buffer, error) {
	if err := FilesReader(c.Fs, c.Dir+"/", c.Files); err != nil {
		return bytes.Buffer{}, err
	}

	// static.Templates is used as partials are always available there
	if err := FilesReader(static.Templates, "", partialsFiles); err != nil {
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

	l5dPartials := []*loader.BufferedFile{}
	for _, template := range L5dPartials {
		l5dPartials = append(l5dPartials, &loader.BufferedFile{
			Name: template,
		})
	}

	return c.render(l5dPartials)
}

// RenderCNI returns a bytes buffer with the result of rendering a Helm chart
func (c *Chart) RenderCNI() (bytes.Buffer, error) {
	cniPartials := []*loader.BufferedFile{
		{Name: "charts/partials/" + chartutil.ChartfileName},
		{Name: "charts/partials/templates/_helpers.tpl"},
		{Name: "charts/partials/templates/_metadata.tpl"},
		{Name: "charts/partials/templates/_pull-secrets.tpl"},
	}
	return c.render(cniPartials)
}

// RenderNoPartials returns a bytes buffer with the result of rendering a Helm chart with no partials
func (c *Chart) RenderNoPartials() (bytes.Buffer, error) {
	return c.render([]*loader.BufferedFile{})
}

// ReadFile updates the buffered file with the data read from disk
func ReadFile(fs http.FileSystem, dir string, f *loader.BufferedFile) error {
	filename := dir + f.Name
	if dir == "" {
		filename = filename[7:]
	}
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
func FilesReader(fs http.FileSystem, dir string, files []*loader.BufferedFile) error {
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
	dataWithVersion := strings.Replace(string(data), versionPlaceholder, version.Version, -1)
	return []byte(dataWithVersion)
}

// InsertVersionValues returns the chart values with the version placeholder
// replaced with the current version.
func InsertVersionValues(values chartutil.Values) (chartutil.Values, error) {
	raw, err := values.YAML()
	if err != nil {
		return nil, err
	}
	return chartutil.ReadValues(InsertVersion([]byte(raw)))
}

// OverrideFromFile overrides the given map with the given file from FS
func OverrideFromFile(values map[string]interface{}, fs http.FileSystem, chartName, name string) (map[string]interface{}, error) {
	// Load Values file
	valuesOverride := loader.BufferedFile{
		Name: name,
	}
	if err := ReadFile(fs, chartName+"/", &valuesOverride); err != nil {
		return nil, err
	}

	var valuesOverrideMap map[string]interface{}
	err := yaml.Unmarshal(valuesOverride.Data, &valuesOverrideMap)
	if err != nil {
		return nil, err
	}
	return MergeMaps(valuesOverrideMap, values), nil
}

// MergeMaps returns the resultant map after merging given two maps of type map[string]interface{}
// The inputs are not mutated and the second map i.e b's values take predence during merge.
// This gives semantically correct merge compared with `mergo.Merge` (with boolean values).
// See https://github.com/imdario/mergo/issues/129
func MergeMaps(a, b map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(a))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v, ok := v.(map[string]interface{}); ok {
			if av, ok := out[k]; ok {
				if av, ok := av.(map[string]interface{}); ok {
					out[k] = MergeMaps(av, v)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}
