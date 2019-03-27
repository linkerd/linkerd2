package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/inject"
	"github.com/spf13/cobra"
)

type resourceTransformerUninject struct {
	configs *config.All
}

type resourceTransformerUninjectSilent struct {
	configs *config.All
}

func runUninjectCmd(inputs []io.Reader, errWriter, outWriter io.Writer, conf *config.All) int {
	return transformInput(inputs, errWriter, outWriter, resourceTransformerUninject{conf})
}

func runUninjectSilentCmd(inputs []io.Reader, errWriter, outWriter io.Writer, conf *config.All) int {
	return transformInput(inputs, errWriter, outWriter, resourceTransformerUninjectSilent{conf})
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

func (rt resourceTransformerUninject) transform(bytes []byte) ([]byte, []inject.Report, error) {
	conf := inject.NewResourceConfig(rt.configs, inject.OriginWebhook)

	report, err := conf.ParseMetaAndYAML(bytes)
	if err != nil {
		return nil, nil, err
	}

	output, err := conf.Uninject(report)
	if err != nil {
		return nil, nil, err
	}
	if output == nil {
		output = bytes
		report.UnsupportedResource = true
	}

	return output, []inject.Report{*report}, nil
}

func (rt resourceTransformerUninjectSilent) transform(bytes []byte) ([]byte, []inject.Report, error) {
	return resourceTransformerUninject(rt).transform(bytes)
}

func (resourceTransformerUninject) generateReport(reports []inject.Report, output io.Writer) {
	// leading newline to separate from yaml output on stdout
	output.Write([]byte("\n"))

	for _, r := range reports {
		if r.Uninjected.Proxy || r.Uninjected.ProxyInit {
			output.Write([]byte(fmt.Sprintf("%s \"%s\" uninjected\n", r.Kind, r.Name)))
		} else {
			if r.Kind != "" {
				output.Write([]byte(fmt.Sprintf("%s \"%s\" skipped\n", r.Kind, r.Name)))
			} else {
				output.Write([]byte(fmt.Sprintf("document missing \"kind\" field, skipped\n")))
			}
		}
	}

	// trailing newline to separate from kubectl output if piping
	output.Write([]byte("\n"))
}

func (resourceTransformerUninjectSilent) generateReport(reports []inject.Report, output io.Writer) {
}
