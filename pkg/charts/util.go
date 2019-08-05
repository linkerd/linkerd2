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
	// Read templates into bytes
	for _, f := range chart.Files {
		data, err := readIntoBytes(chart.Dir + "/" + f.Name)
		if err != nil {
			return bytes.Buffer{}, err
		}
		f.Data = data
	}

	// Create chart and render templates
	chrt, err := chartutil.LoadFiles(chart.Files)
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

func readIntoBytes(filename string) ([]byte, error) {
	// TODO: remove `chart/` after `linkerd install` starts using `/charts`
	file, err := static.Templates.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(file)

	return buf.Bytes(), nil
}
