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
type logCmdConfig struct {
	clientset *kubernetes.Clientset
	*stern.Config
}

type logsOptions struct {
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

func newLogsOptions() *logsOptions {
	return &logsOptions{
		containerFilter: "",
		namespace:       controlPlaneNamespace,
		podFilter:       "",
		containerState:  "running",
		labelSelector:   "",
		sinceSeconds:    48 * time.Hour,
		tail:            -1,
		timestamps:      false,
	}
}

func (o *logsOptions) toSternConfig() (*stern.Config, error) {
	config := &stern.Config{}

	podFilterRgx, err := regexp.Compile(o.podFilter)
	if err != nil {
		return nil, err
	}
	config.PodQuery = podFilterRgx

	containerFilterRgx, err := regexp.Compile(o.containerFilter)
	if err != nil {
		return nil, err
	}
	config.ContainerQuery = containerFilterRgx

	containerState, err := stern.NewContainerState(o.containerState)
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

	if o.labelSelector == "" {
		config.LabelSelector = labels.Everything()
	} else {
		selector, err := labels.Parse(o.labelSelector)
		if err != nil {
			return nil, err
		}
		config.LabelSelector = selector
	}

	if o.tail != -1 {
		config.TailLines = &o.tail
	}

	config.Since = o.sinceSeconds
	config.Timestamps = o.timestamps
	config.Namespace = o.namespace

	return config, nil
}

func newLogCmdConfig(options *logsOptions, kubeconfigPath, kubeContext string) (*logCmdConfig, error) {
	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(kubeAPI.Config)
	if err != nil {
		return nil, err
	}

	c, err := options.toSternConfig()
	if err != nil {
		return nil, err
	}

	return &logCmdConfig{
		clientset,
		c,
	}, nil
}

func newCmdLogs() *cobra.Command {
	options := newLogsOptions()

	cmd := &cobra.Command{
		Use:   "logs [flags]",
		Short: "Tail logs from kubernetes resource",
		Long:  `Tail logs from kubernetes resource.`,
		Example: `  # Tail logs from all containers in pods that have the prefix 'linkerd-controller' in the linkerd namespace
  linkerd logs --pod-filter linkerd-controller.* --namespace linkerd

  # Tail logs from the linkerd-proxy container in the grafana pod within the linkerd control plane
  linkerd logs --pod-filter linkerd-grafana.* --container linkerd-proxy --namespace linkerd
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := newLogCmdConfig(options, kubeconfigPath, kubeContext)

			if err != nil {
				return err
			}

			return runLogOutput(opts)
		},
	}

	cmd.PersistentFlags().StringVarP(&options.containerFilter, "container", "c", options.containerFilter, "Regex string to use for filtering log lines by container name")
	cmd.PersistentFlags().StringVar(&options.labelSelector, "label-selector", options.labelSelector, "kubernetes label selector to retrieve logs from resources that match")
	cmd.PersistentFlags().StringVar(&options.containerState, "container-state", options.containerState, "Show logs from containers that are in a specific container state")
	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "String to retrieve logs from pods in the given namespace")
	cmd.PersistentFlags().StringVarP(&options.podFilter, "pod-filter", "p", options.podFilter, "Regex string to use for filtering log lines by pod name")
	cmd.PersistentFlags().DurationVarP(&options.sinceSeconds, "since", "s", options.sinceSeconds, "Duration of how far back logs should be retrieved")
	cmd.PersistentFlags().Int64Var(&options.tail, "tail", options.tail, "Last number of log lines to show for a given container. -1 does not show any previous log lines")
	cmd.PersistentFlags().BoolVarP(&options.timestamps, "timestamps", "t", options.timestamps, "Print timestamps for each given log line")

	return cmd
}

func runLogOutput(opts *logCmdConfig) error {
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
		for a := range added {
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
	}()

	<-sigCh
	return nil
}
