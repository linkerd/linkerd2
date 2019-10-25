package charts

import (
	"bytes"
	"path"

	"github.com/linkerd/linkerd2/pkg/charts/static"
	"k8s.io/helm/pkg/chartutil"
	helmChart "k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/renderutil"
	"k8s.io/helm/pkg/timeconv"
)

// Chart holds the necessary info to render a Helm chart
type Chart struct {
	Name      string
	Dir       string
	Namespace string
	RawValues []byte
	Files     []*chartutil.BufferedFile
}

// Render returns a bytes buffer with the result of rendering a Helm chart
func (chart *Chart) Render() (bytes.Buffer, error) {
	if err := filesReader(chart.Dir+"/", chart.Files); err != nil {
		return bytes.Buffer{}, err
	}

	// Keep this slice synced with the contents of /charts/partials
	partialsFiles := []*chartutil.BufferedFile{
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
	}
	if err := filesReader("", partialsFiles); err != nil {
		return bytes.Buffer{}, err
	}

	// Create chart and render templates
	chrt, err := chartutil.LoadFiles(append(chart.Files, partialsFiles...))
	if err != nil {
		return bytes.Buffer{}, err
	}

	renderOpts := renderutil.Options{
		ReleaseOptions: chartutil.ReleaseOptions{
			Name:      chart.Name,
			IsInstall: true,
			IsUpgrade: false,
			Time:      timeconv.Now(),
			Namespace: chart.Namespace,
		},
		KubeVersion: "",
	}

	chrtConfig := &helmChart.Config{Raw: string(chart.RawValues), Values: map[string]*helmChart.Value{}}
	renderedTemplates, err := renderutil.Render(chrt, chrtConfig, renderOpts)
	if err != nil {
		return bytes.Buffer{}, err
	}

	// Merge templates and inject
	var buf bytes.Buffer
	for _, tmpl := range chart.Files {
		t := path.Join(renderOpts.ReleaseOptions.Name, tmpl.Name)
		if _, err := buf.WriteString(renderedTemplates[t]); err != nil {
			return bytes.Buffer{}, err
		}
	}

	return buf, nil
}

func filesReader(dir string, files []*chartutil.BufferedFile) error {
	for _, f := range files {
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
		buf.ReadFrom(file)

		f.Data = buf.Bytes()
	}
	return nil
}
