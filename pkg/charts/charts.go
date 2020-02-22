package charts

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"path"
	fpath "path/filepath"
	"strings"

	"github.com/linkerd/linkerd2/pkg/charts/static"
	"github.com/linkerd/linkerd2/pkg/version"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/chart"
	helmChart "k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/renderutil"
	"k8s.io/helm/pkg/timeconv"
)

const (
	versionPlaceholder  = "{version}"
	linkerdChartName = "linkerd2"
	linkerdChartRootDir = "linkerd2"
)

// Chart holds the necessary info to render a Helm chart
type Chart struct {
	Name      string
	Dir       string
	Namespace string
	RawValues []byte
	Files     []*chartutil.BufferedFile
}

// LoadChart returns a new Chart object that contains all the files of the
// specified chart. The chart's files are loaded from the virtual filesystem
// using the Helm's chartutil.LoadFiles() helper function.
func LoadChart(chartName string) (*helmChart.Chart, error) {
	var vfiles []*chartutil.BufferedFile

	// retrieve all the files of a chart located at linkerdChartRootRid
	walkVFS := func() error {
		files, err := readVirtualFiles(linkerdChartRootDir, chartName)
		if err != nil {
			return err
		}

		for path, file := range files {
			data, err := ioutil.ReadAll(file)
			if err != nil {
				return err
			}

			filename := path
			if strings.HasPrefix(path, chartName) {
				filename = path[len(chartName)+1:]
			}

			vfiles = append(vfiles, &chartutil.BufferedFile{
				Name: filename,
				Data: data,
			})
		}

		return nil
	}

	if err := walkVFS(); err != nil {
		return nil, err
	}

	return chartutil.LoadFiles(vfiles)
}

// LoadDependencies loads all the dependent subcharts of the specified chart.
// It relies on LoadChart to load the files and metadata of the chart from the
// VFS.
func LoadDependencies(chartName string) ([]*chart.Chart, error) {
	chart, err := LoadChart(chartName)
	if err != nil {
		return nil, err
	}

	return chart.Dependencies, nil
}

// readVirtualFiles read the content of a file from the VFS. If the file is
// directory, it also loads the children files content, recursively.
//
// The result map is keyed off the full path of the files, which is needed
// by the renderer. The http.File struct contains only the file basename.
func readVirtualFiles(filename, root string) (map[string]http.File, error) {
	filepath := filename
	if !strings.HasPrefix(filepath, root) {
		filepath = fpath.Join(root, filename)
	}

	file, err := static.Templates.Open(filepath)
	if err != nil {
		return nil, err
	}

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}

	if !fileInfo.IsDir() {
		return map[string]http.File{filepath: file}, nil
	}

	// file is a directory. read its children files.
	files := map[string]http.File{}
	filesInfo, err := file.Readdir(0)
	if err != nil {
		return nil, err
	}

	parent := fileInfo.Name()
	for _, fileInfo := range filesInfo {
		filename := fpath.Join(parent, fileInfo.Name())
		children, err := readVirtualFiles(filename, root)
		if err != nil {
			return nil, err
		}

		for path, file := range children {
			files[path] = file
		}
	}

	return files, nil
}

func (chart *Chart) render(partialsFiles []*chartutil.BufferedFile) (bytes.Buffer, error) {
	if err := FilesReader(chart.Dir+"/", chart.Files); err != nil {
		return bytes.Buffer{}, err
	}

	if err := FilesReader("", partialsFiles); err != nil {
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

// Render returns a bytes buffer with the result of rendering a Helm chart
func (chart *Chart) Render() (bytes.Buffer, error) {

	// Keep this slice synced with the contents of /charts/partials
	l5dPartials := []*chartutil.BufferedFile{
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
		{Name: "charts/partials/templates/_affinity.tpl"},
		{Name: "charts/partials/templates/_validate.tpl"},
	}
	return chart.render(l5dPartials)
}

// RenderCNI returns a bytes buffer with the result of rendering a Helm chart
func (chart *Chart) RenderCNI() (bytes.Buffer, error) {
	cniPartials := []*chartutil.BufferedFile{
		{Name: "charts/partials/" + chartutil.ChartfileName},
		{Name: "charts/partials/templates/_helpers.tpl"},
	}
	return chart.render(cniPartials)
}

// ReadFile updates the buffered file with the data read from disk
func ReadFile(dir string, f *chartutil.BufferedFile) error {
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
func FilesReader(dir string, files []*chartutil.BufferedFile) error {
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
