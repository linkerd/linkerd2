package charts

import (
	"bytes"
	"embed"
	"errors"
	"path"
	"strings"

	"github.com/linkerd/linkerd2/charts"
	"github.com/linkerd/linkerd2/pkg/version"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"sigs.k8s.io/yaml"
)

const versionPlaceholder = "linkerdVersionValue"

var (
	// L5dPartials is the list of templates in partials chart
	// Keep this slice synced with the contents of charts/partials
	l5dPartials = []string{
		"partials/" + chartutil.ChartfileName,
		"partials/templates/_affinity.tpl",
		"partials/templates/_capabilities.tpl",
		"partials/templates/_debug.tpl",
		"partials/templates/_helpers.tpl",
		"partials/templates/_metadata.tpl",
		"partials/templates/_nodeselector.tpl",
		"partials/templates/_network-validator.tpl",
		"partials/templates/_proxy-config-ann.tpl",
		"partials/templates/_proxy-init.tpl",
		"partials/templates/_proxy.tpl",
		"partials/templates/_pull-secrets.tpl",
		"partials/templates/_resources.tpl",
		"partials/templates/_tolerations.tpl",
		"partials/templates/_trace.tpl",
		"partials/templates/_validate.tpl",
		"partials/templates/_volumes.tpl",
	}
)

// Chart holds the necessary info to render a Helm chart
type Chart struct {
	Name      string
	Dir       string
	Namespace string

	// RawValues are yaml-formatted values entries. Either this or Values
	// should be set, but not both
	RawValues []byte

	// Values are the config key-value entries. Either this or RawValues should
	// be set, but not both
	Values map[string]any

	Files []*loader.BufferedFile
	Fs    embed.FS
}

func (c *Chart) render(partialsFiles []*loader.BufferedFile) (bytes.Buffer, error) {
	if err := FilesReader(c.Fs, c.Dir+"/", c.Files); err != nil {
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

	if len(c.RawValues) > 0 {
		if c.Values != nil {
			return bytes.Buffer{}, errors.New("either RawValues or Values should be set, but not both")
		}
		err = yaml.Unmarshal(c.RawValues, &c.Values)
		if err != nil {
			return bytes.Buffer{}, err
		}
	}

	valuesToRender, err := chartutil.ToRenderValues(chart, c.Values, releaseOptions, nil)
	if err != nil {
		return bytes.Buffer{}, err
	}
	release, _ := valuesToRender["Release"].(map[string]interface{})
	release["Service"] = "CLI"

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

	partials, err := LoadPartials()
	if err != nil {
		return bytes.Buffer{}, err
	}

	return c.render(partials)
}

// RenderCNI returns a bytes buffer with the result of rendering a Helm chart
func (c *Chart) RenderCNI() (bytes.Buffer, error) {
	cniPartials := []*loader.BufferedFile{
		{Name: "partials/" + chartutil.ChartfileName},
		{Name: "partials/templates/_helpers.tpl"},
		{Name: "partials/templates/_metadata.tpl"},
		{Name: "partials/templates/_pull-secrets.tpl"},
		{Name: "partials/templates/_tolerations.tpl"},
		{Name: "partials/templates/_resources.tpl"},
	}

	// Load all partial chart files into buffer
	if err := FilesReader(charts.Templates, "", cniPartials); err != nil {
		return bytes.Buffer{}, err
	}

	// The partials files must have the "charts/" prefix to be loaded correctly.
	for i, f := range cniPartials {
		cniPartials[i].Name = path.Join("charts", f.Name)
	}
	return c.render(cniPartials)
}

// ReadFile updates the buffered file with the data read from disk
func ReadFile(fs embed.FS, dir string, f *loader.BufferedFile) error {
	file, err := fs.Open(dir + f.Name)
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
func FilesReader(fs embed.FS, dir string, files []*loader.BufferedFile) error {
	for _, f := range files {
		if err := ReadFile(fs, dir, f); err != nil {
			return err
		}
	}
	return nil
}

func LoadPartials() ([]*loader.BufferedFile, error) {
	var partialFiles []*loader.BufferedFile
	for _, template := range l5dPartials {
		partialFiles = append(partialFiles,
			&loader.BufferedFile{Name: template},
		)
	}

	// Load all partial chart files into buffer
	if err := FilesReader(charts.Templates, "", partialFiles); err != nil {
		return nil, err
	}

	// The partials files must have the "charts/" prefix to be loaded correctly.
	for i, f := range partialFiles {
		partialFiles[i].Name = path.Join("charts", f.Name)
	}
	return partialFiles, nil
}

// InsertVersion returns the chart values file contents passed in
// with the version placeholder replaced with the current version
func InsertVersion(data []byte) []byte {
	dataWithVersion := strings.ReplaceAll(string(data), versionPlaceholder, version.Version)
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
func OverrideFromFile(values map[string]interface{}, fs embed.FS, chartName, name string) (map[string]interface{}, error) {
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
// The inputs are not mutated and the second map i.e b's values take precedence during merge.
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
