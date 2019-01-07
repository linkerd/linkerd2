package cmd

import (
	"context"
	"os"
	"os/signal"
	"regexp"
	"text/template"
	"time"

	"github.com/fatih/color"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	"github.com/wercker/stern/stern"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)
//This code replicates most of the functionality in https://github.com/wercker/stern/blob/master/cmd/cli.go
type logCmdOpts struct {
	clientset *kubernetes.Clientset
	*stern.Config
}

type commandFlags struct {
	containerFilter string
	namespace       string
	podFilter       string
	containerState  string
	labelSelector   string
	sinceSeconds    time.Duration
	tail            int64
	timestamps      bool
	follow          bool
}

func (c *commandFlags) NewTailConfig() (*stern.Config, error) {
	config := &stern.Config{}

	podFilterRgx, err := regexp.Compile(c.podFilter)
	if err != nil {
		return nil, err
	}
	config.PodQuery = podFilterRgx

	containerFilterRgx, err := regexp.Compile(c.containerFilter)
	if err != nil {
		return nil, err
	}
	config.ContainerQuery = containerFilterRgx

	containerState, err := stern.NewContainerState(c.containerState)
	if err != nil {
		return nil, err
	}
	config.ContainerState = containerState

	funcs := make(map[string]interface{})

	funcs["color"] = func(color color.Color, text string) string {
		return color.SprintFunc()(text)
	}

	tmpl, err := template.New("logs").
		Funcs(funcs).Parse("{{color .PodColor .PodName}} {{color .ContainerColor .ContainerName}} {{.Message}}")
	if err != nil {
		return nil, err
	}
	config.Template = tmpl

	selector, err := labels.Parse(c.labelSelector)
	if err != nil {
		return nil, err
	}

	if c.labelSelector == "" {
		config.LabelSelector = labels.Everything()
	}
	config.LabelSelector = selector

	if c.tail != -1 {
		config.TailLines = &c.tail
	}

	config.Since = c.sinceSeconds
	config.Timestamps = c.timestamps
	config.Namespace = c.namespace

	return config, nil
}

func newLogOptions(flags *commandFlags, kubeconfigPath, kubeContext string) (*logCmdOpts, error) {
	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(kubeAPI.Config)
	if err != nil {
		return nil, err
	}

	c, err := flags.NewTailConfig()
	if err != nil {
		return nil, err
	}

	return &logCmdOpts{
		clientset,
		c,
	}, nil
}

func newCmdLogs() *cobra.Command {

	flags := &commandFlags{}

	cmd := &cobra.Command{
		Use:   "logs [flags]",
		Short: "Tail logs from kubernetes resource",
		Long:  `Tail logs from kubernetes resource.`,
		Example:`# Tail logs from all containers in pods that have the prefix 'linkerd-controller' in the linkerd namespace
  linkerd logs --pod-filter linkerd-controller.* --namespace linkerd

  # Tail logs from the linkerd-proxy container in the grafana pod within the linkerd control plane
  linkerd logs --pod-filter linkerd-grafana.* --container-filter linkerd-proxy --namespace linkerd
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := newLogOptions(flags, kubeconfigPath, kubeContext)

			if err != nil {
				return err
			}

			return runLogOutput(opts)
		},
	}

	cmd.PersistentFlags().StringVarP(&flags.containerFilter, "container-filter", "c", "", "Regex string to use for filtering log lines by container name")
	cmd.PersistentFlags().StringVar(&flags.labelSelector, "label-selector", "", "kubernetes label selector to retrieve logs from resources that match")
	cmd.PersistentFlags().StringVar(&flags.containerState, "container-state", "running", "Show logs from containers that are in a specific container state")
	cmd.PersistentFlags().StringVarP(&flags.namespace, "namespace", "n", controlPlaneNamespace, "String to retrieve logs from pods in the given namespace")
	cmd.PersistentFlags().StringVarP(&flags.podFilter, "pod-filter", "p", "", "Regex string to use for filtering log lines by pod name")
	cmd.PersistentFlags().DurationVarP(&flags.sinceSeconds, "since", "s", 172800*time.Second, "Duration of how far back logs should be retrieved")
	cmd.PersistentFlags().Int64Var(&flags.tail, "tail", -1, "Last number of log lines to show for a given container. -1 does not show any previous log lines")
	cmd.PersistentFlags().BoolVarP(&flags.timestamps, "timestamps", "t", false, "Print timestamps for each given log line")

	return cmd
}

func runLogOutput(opts *logCmdOpts) error {

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, os.Kill)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	podInterface := opts.clientset.CoreV1().Pods(opts.Namespace)
	tails := make(map[string]*stern.Tail)

	added, _, err := stern.Watch(
		ctx,
		podInterface,
		opts.PodQuery,
		opts.ContainerQuery,
		nil,
		opts.ContainerState,
		opts.LabelSelector,
	)

	if err != nil {
		return err
	}

	go func() {
		for {
			select {
			case a := <-added:
				if a != nil {
					tailOpts := &stern.TailOptions{
						SinceSeconds: int64(opts.Since.Seconds()),
						Timestamps:   opts.Timestamps,
						TailLines:    opts.TailLines,
						Namespace:    true,
					}

					newTail := stern.NewTail(a.Namespace, a.Pod, a.Container, opts.Template, tailOpts)
					if _, ok := tails[a.GetID()]; !ok {
						tails[a.GetID()] = newTail
					}
					newTail.Start(ctx, podInterface)
				}
			}
		}
	}()

	<-sigCh
	return nil
}
