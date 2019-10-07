package cmd

import (
	"fmt"
	"io"
	"os"

	cfg "github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/inject"
	"github.com/spf13/cobra"
)

func newCmdDebugSidecar() *cobra.Command {
	options := &proxyConfigOptions{}

	run := func(inject bool) func(cmd *cobra.Command, args []string) error {
		return func(cmd *cobra.Command, args []string) error {

			if len(args) < 1 {
				return fmt.Errorf("please specify a kubernetes resource file")
			}

			in, err := read(args[0])
			if err != nil {
				return err
			}

			configs, err := options.fetchConfigsOrDefault()
			if err != nil {
				return err
			}
			transformer := &resourceTransformerDebugSidecar{
				configs: configs,
				inject:  inject,
			}
			exitCode := runDebugSidecarCmd(in, stderr, stdout, transformer)
			os.Exit(exitCode)
			return nil
		}
	}

	root := &cobra.Command{
		Use:   "debug-sidecar [inject | uninject] CONFIG-FILE",
		Short: "Add debug sidecar or remove it from meshed pods",
		Long: `Add debug sidecar or remove it from meshed pods.

You can inject or uninject the debug sidecar into resources contained in a single 
file, inside a folder and its sub-folders, or coming from stdin.`,

		Example: `  # Inject the debug sidecar into all the deployments in the default namespace.
  kubectl get deploy -o yaml | linkerd debug-sidecar inject - | kubectl apply -f -

  # Download a resource and inject the debug sidecar  it through stdin.
  curl http://url.to/yml | linkerd debug-sidecar inject - | kubectl apply -f -

  # Uninject the debug sidecar from all the resources inside a folder and its sub-folders.
  linkerd debug-sidecar uninject <folder> | kubectl apply -f -`,
	}

	inject := &cobra.Command{
		Use:   "inject CONFIG-FILE",
		Short: "Adds the debug sidecar to meshed pods.",
		Long:  "Adds the debug sidecar to meshed pods.",
		RunE:  run(true),
	}

	uninject := &cobra.Command{
		Use:   "uninject CONFIG-FILE",
		Short: "Removes the debug sidecar from meshed pods.",
		Long:  "Removes the debug sidecar from meshed pods.",
		RunE:  run(false),
	}

	root.AddCommand(inject)
	root.AddCommand(uninject)
	return root
}

type resourceTransformerDebugSidecar struct {
	configs *cfg.All
	inject  bool
}

func runDebugSidecarCmd(inputs []io.Reader, errWriter, outWriter io.Writer, transformer *resourceTransformerDebugSidecar) int {
	return transformInput(inputs, errWriter, outWriter, transformer)
}

func writeErrors(r inject.Report, output io.Writer) {
	if r.Kind != "" {
		output.Write([]byte(fmt.Sprintf("%s \"%s\" skipped\n", r.Kind, r.Name)))
	} else {
		output.Write([]byte(fmt.Sprintf("document missing \"kind\" field, skipped\n")))
	}
}

func writeResult(result string, r inject.Report, output io.Writer) {
	output.Write([]byte(fmt.Sprintf("%s \"%s\" debug sidecar %s\n", r.Kind, r.Name, result)))
}

func (rt resourceTransformerDebugSidecar) generateReport(reports []inject.Report, output io.Writer) {
	// leading newline to separate from yaml output on stdout
	output.Write([]byte("\n"))

	for _, r := range reports {
		if rt.inject {
			if r.CanInjectinjectDebugSidecar() {
				writeResult("injected", r, output)
			} else {
				writeErrors(r, output)
			}
		} else {
			if r.Uninjected.DebugSidecar {
				writeResult("uninjected", r, output)
			} else {
				writeErrors(r, output)
			}
		}
	}
	// trailing newline to separate from kubectl output if piping
	output.Write([]byte("\n"))
}

func (rt resourceTransformerDebugSidecar) transform(bytes []byte) ([]byte, []inject.Report, error) {

	conf := inject.NewResourceConfig(rt.configs, inject.OriginCLI)

	report, err := conf.ParseMetaAndYAML(bytes)
	if err != nil {
		return nil, nil, err
	}

	if !conf.IsControlPlaneComponent() {
		return nil, nil, fmt.Errorf("cannot use debug-sidecar command on non linkerd components")
	}

	var output []byte
	if rt.inject {
		output, err = conf.InjectDebug(report)
	} else {
		output, err = conf.UnInjectDebug(report)
	}
	if err != nil {
		return nil, nil, err
	}
	if output == nil {
		output = bytes
		report.UnsupportedResource = true
	}

	return output, []inject.Report{*report}, nil
}
