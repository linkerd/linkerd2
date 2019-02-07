package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

type resourceTransformerUninject struct{}

type resourceTransformerUninjectSilent struct{}

// UninjectYAML processes resource definitions and outputs them after uninjection in out
func UninjectYAML(in io.Reader, out io.Writer, report io.Writer, options *injectOptions) error {
	return ProcessYAML(in, out, report, options, resourceTransformerUninject{})
}

func runUninjectCmd(inputs []io.Reader, errWriter, outWriter io.Writer, options *injectOptions) int {
	return transformInput(inputs, errWriter, outWriter, options, resourceTransformerUninject{})
}

func runUninjectSilentCmd(inputs []io.Reader, errWriter, outWriter io.Writer, options *injectOptions) int {
	return transformInput(inputs, errWriter, outWriter, options, resourceTransformerUninjectSilent{})
}

func newCmdUninject() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninject [flags] CONFIG-FILE",
		Short: "Remove the Linkerd proxy from a Kubernetes config",
		Long: `Remove the Linkerd proxy from a Kubernetes config.

You can uninject resources contained in a single file, inside a folder and its
sub-folders, or coming from stdin.`,
		Example: `  # Uninject all the deployments in the default namespace.
  kubectl get deploy -o yaml | linkerd uninject - | kubectl apply -f -

  # Download a resource and uninject it through stdin.
  curl http://url.to/yml | linkerd uninject - | kubectl apply -f -

  # Uninject all the resources inside a folder and its sub-folders.
  linkerd uninject <folder> | kubectl apply -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {

			if len(args) < 1 {
				return fmt.Errorf("please specify a kubernetes resource file")
			}

			in, err := read(args[0])
			if err != nil {
				return err
			}

			exitCode := runUninjectCmd(in, os.Stderr, os.Stdout, nil)
			os.Exit(exitCode)
			return nil
		},
	}

	return cmd
}

func (rt resourceTransformerUninject) transform(bytes []byte, options *injectOptions) ([]byte, []injectReport, error) {
	conf := &resourceConfig{}
	output, reports, err := conf.parse(bytes, options, rt)
	if output != nil || err != nil {
		return output, reports, err
	}

	report := injectReport{
		kind: strings.ToLower(conf.meta.Kind),
		name: conf.om.Name,
	}

	// If we don't uninject anything into the pod template then output the
	// original serialization of the original object. Otherwise, output the
	// serialization of the modified object.
	output = bytes
	if conf.podSpec != nil {
		uninjectPodSpec(conf.podSpec, &report)
		uninjectObjectMeta(conf.objectMeta)
		var err error
		output, err = yaml.Marshal(conf.obj)
		if err != nil {
			return nil, nil, err
		}
	} else {
		report.unsupportedResource = true
	}

	return output, []injectReport{report}, nil
}

func (rt resourceTransformerUninjectSilent) transform(bytes []byte, options *injectOptions) ([]byte, []injectReport, error) {
	return resourceTransformerUninject{}.transform(bytes, options)
}

func (resourceTransformerUninject) generateReport(uninjectReports []injectReport, output io.Writer) {
	// leading newline to separate from yaml output on stdout
	output.Write([]byte("\n"))

	for _, r := range uninjectReports {
		if r.sidecar {
			output.Write([]byte(fmt.Sprintf("%s \"%s\" uninjected\n", r.kind, r.name)))
		} else {
			output.Write([]byte(fmt.Sprintf("%s \"%s\" skipped\n", r.kind, r.name)))
		}
	}

	// trailing newline to separate from kubectl output if piping
	output.Write([]byte("\n"))
}

func (resourceTransformerUninjectSilent) generateReport(uninjectReports []injectReport, output io.Writer) {
}

// Given a PodSpec, update the PodSpec in place with the sidecar
// and init-container uninjected
func uninjectPodSpec(t *v1.PodSpec, report *injectReport) {
	initContainers := []v1.Container{}
	for _, container := range t.InitContainers {
		if container.Name != k8s.InitContainerName {
			initContainers = append(initContainers, container)
		} else {
			report.sidecar = true
		}
	}
	t.InitContainers = initContainers

	containers := []v1.Container{}
	for _, container := range t.Containers {
		if container.Name != k8s.ProxyContainerName {
			containers = append(containers, container)
		}
	}
	t.Containers = containers

	volumes := []v1.Volume{}
	for _, volume := range t.Volumes {
		// TODO: move those strings to constants
		if volume.Name != k8s.TLSTrustAnchorVolumeName && volume.Name != k8s.TLSSecretsVolumeName {
			volumes = append(volumes, volume)
		}
	}
	t.Volumes = volumes
}

func uninjectObjectMeta(t *metaV1.ObjectMeta) {
	newAnnotations := make(map[string]string)
	for key, val := range t.Annotations {
		if key != k8s.CreatedByAnnotation && key != k8s.ProxyVersionAnnotation {
			newAnnotations[key] = val
		}
	}
	t.Annotations = newAnnotations

	labels := make(map[string]string)
	for key, val := range t.Labels {
		keep := true
		for _, label := range k8s.InjectedLabels {
			if key == label {
				keep = false
				break
			}
		}
		if keep {
			labels[key] = val
		}
	}
	t.Labels = labels
}
