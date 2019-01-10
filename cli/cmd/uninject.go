package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type resourceTransformerUninject struct{}

func newCmdUninject() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninject [flags] CONFIG-FILE",
		Short: "Remove the Linkerd proxy from a Kubernetes config",
		Long: `Remove the Linkerd proxy from a Kubernetes config.

You can use a config file from stdin by using the '-' argument
with 'linkerd uninject'. e.g. curl http://url.to/yml | linkerd uninject -
Also works with a folder containing resource files and other
sub-folder. e.g. linkerd uninject <folder> | kubectl apply -f -
`,
		RunE: func(cmd *cobra.Command, args []string) error {

			if len(args) < 1 {
				return fmt.Errorf("please specify a kubernetes resource file")
			}

			in, err := read(args[0])
			if err != nil {
				return err
			}

			exitCode := transformInput(in, os.Stderr, os.Stdout, nil, resourceTransformerUninject{})
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
		name: fmt.Sprintf("%s/%s", strings.ToLower(conf.meta.Kind), conf.om.Name),
	}

	// If we don't uninject anything into the pod template then output the
	// original serialization of the original object. Otherwise, output the
	// serialization of the modified object.
	output = bytes
	if conf.podSpec != nil {
		uninjectPodSpec(conf.podSpec)
		uninjectObjectMeta(conf.objectMeta, conf.k8sLabels)
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

func (resourceTransformerUninject) generateReport(uninjectReports []injectReport, output io.Writer) {
	uninjected := []string{}
	for _, r := range uninjectReports {
		if !r.unsupportedResource {
			uninjected = append(uninjected, r.name)
		}
	}
	summary := fmt.Sprintf("Summary: %d of %d YAML document(s) uninjected", len(uninjected), len(uninjectReports))
	output.Write([]byte(fmt.Sprintf("\n%s\n", summary)))

	for _, i := range uninjected {
		output.Write([]byte(fmt.Sprintf("  %s\n", i)))
	}

	// trailing newline to separate from kubectl output if piping
	output.Write([]byte("\n"))
}

// Given a PodSpec, update the PodSpec in place with the sidecar
// and init-container uninjected
func uninjectPodSpec(t *v1.PodSpec) {
	initContainers := []v1.Container{}
	for _, container := range t.InitContainers {
		if container.Name != k8s.InitContainerName {
			initContainers = append(initContainers, container)
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
		if volume.Name != "linkerd-trust-anchors" && volume.Name != "linkerd-secrets" {
			volumes = append(volumes, volume)
		}
	}
	t.Volumes = volumes
}

func uninjectObjectMeta(t *metaV1.ObjectMeta, k8sLabels map[string]string) {
	newAnnotations := make(map[string]string)
	for key, val := range t.Annotations {
		if key != k8s.CreatedByAnnotation && key != k8s.ProxyVersionAnnotation {
			newAnnotations[key] = val
		}
	}
	t.Annotations = newAnnotations

	labels := make(map[string]string)
	ldLabels := []string{k8s.ControllerNSLabel}
	for key := range k8sLabels {
		ldLabels = append(ldLabels, key)
	}
	for key, val := range t.Labels {
		keep := true
		for _, label := range ldLabels {
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
