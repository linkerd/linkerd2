package cmd

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/fatih/color"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	adminHTTPPortName string = "admin-http"
)

type diagnosticsOptions struct {
	wait    time.Duration
	noColor bool
}

func newDiagnosticsOptions() *diagnosticsOptions {
	return &diagnosticsOptions{
		wait:    30 * time.Second,
		noColor: false,
	}
}

var colorList = [][2]*color.Color{
	{color.New(color.FgHiCyan), color.New(color.FgCyan)},
	{color.New(color.FgHiGreen), color.New(color.FgGreen)},
	{color.New(color.FgHiMagenta), color.New(color.FgMagenta)},
	{color.New(color.FgHiYellow), color.New(color.FgYellow)},
	{color.New(color.FgHiBlue), color.New(color.FgBlue)},
	{color.New(color.FgHiRed), color.New(color.FgRed)},
}

func determineColor(podName string) (podColor, containerColor *color.Color) {
	hash := fnv.New32()
	hash.Write([]byte(podName))
	idx := hash.Sum32() % uint32(len(colorList))

	colors := colorList[idx]
	return colors[0], colors[1]
}

func newCmdDiagnostics() *cobra.Command {
	options := newDiagnosticsOptions()

	cmd := &cobra.Command{
		Use:   "diagnostics",
		Short: "Fetch metrics directly from the Linkerd control plane containers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			color.NoColor = options.noColor

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			pods, err := k8sAPI.CoreV1().Pods(controlPlaneNamespace).List(metav1.ListOptions{})
			if err != nil {
				return err
			}

			results := getMetrics(k8sAPI, pods.Items, adminHTTPPortName, options.wait, verbose)

			var buf bytes.Buffer
			for i, result := range results {
				podColor, containerColor := determineColor(result.pod)
				p := podColor.SprintFunc()
				c := containerColor.SprintFunc()

				content := fmt.Sprintf("#\n# POD %s (%d of %d)\n", p(result.pod), i+1, len(results))
				if result.err != nil {
					content += fmt.Sprintf("# ERROR %s\n", result.err)
				} else {
					content += fmt.Sprintf("# CONTAINER %s \n#\n", c(result.container))
					content += string(result.metrics)
				}
				buf.WriteString(content)
			}
			fmt.Printf("%s", buf.String())

			return nil
		},
	}

	cmd.Flags().DurationVarP(&options.wait, "wait", "w", options.wait, "Time allowed to fetch diagnostics")
	cmd.PersistentFlags().BoolVarP(&options.noColor, "no-color", "n", options.noColor, "Disable colorized output")

	return cmd
}
