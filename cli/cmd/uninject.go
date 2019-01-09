package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newCmdUninject() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninject",
		Short: "Remove the Linkerd proxy from a Kubernetes config",
		Long: `Remove the Linkerd proxy from a Kubernetes config.

You can use a config file from stdin by using the '-' argument
with 'linkerd uninject'. e.g. curl http://url.to/yml | linkerd uninject -
Also works with a folder containing resource files and other
sub-folder. e.g. linkerd uninject <folder> | kubectl apply -f -
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			in, err := read(args[0])
			if err != nil {
				return err
			}

			exitCode := transformInput(in, os.Stderr, os.Stdout, nil, uninjectResource)
			os.Exit(exitCode)
			return nil
		},
	}

	return cmd
}

func uninjectResource(bytes []byte, options *injectOptions) ([]byte, []injectReport, error) {
	conf := &resourceConfig{}
	output, reports, err := conf.parse(bytes, options, uninjectResource)
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
