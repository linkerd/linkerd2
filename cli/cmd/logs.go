package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"time"

	"github.com/fatih/color"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	"github.com/wercker/stern/stern"
	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

//This code replicates most of the functionality in https://github.com/wercker/stern/blob/master/cmd/cli.go
type logCmdConfig struct {
	clientset *kubernetes.Clientset
	*stern.Config
}

type logsOptions struct {
	container             string
	controlPlaneComponent string
	noColor               bool
	sinceSeconds          time.Duration
	tail                  int64
	timestamps            bool
}

func newLogsOptions() *logsOptions {
	return &logsOptions{
		container:             "",
		controlPlaneComponent: "",
		noColor:               false,
		sinceSeconds:          48 * time.Hour,
		tail:                  -1,
		timestamps:            false,
	}
}

func (o *logsOptions) toSternConfig(controlPlaneComponents, availableContainers []string) (*stern.Config, error) {
	config := &stern.Config{}

	if o.controlPlaneComponent == "" {
		config.LabelSelector = labels.Everything()
	} else {
		var podExists string
		for _, p := range controlPlaneComponents {
			if p == o.controlPlaneComponent {
				podExists = p
				break
			}
		}

		if podExists == "" {
			return nil, fmt.Errorf("control plane component [%s] does not exist. Must be one of %v", o.controlPlaneComponent, controlPlaneComponents)
		}
		selector, err := labels.Parse(fmt.Sprintf("linkerd.io/control-plane-component=%s", o.controlPlaneComponent))
		if err != nil {
			return nil, err
		}
		config.LabelSelector = selector
	}

	if o.container != "" {
		var matchingContainer string
		for _, c := range availableContainers {
			if o.container == c {
				matchingContainer = c
				break
			}
		}
		if matchingContainer == "" {
			return nil, fmt.Errorf("container [%s] does not exist in control plane [%s]", o.container, controlPlaneNamespace)
		}
	}

	containerFilterRgx, err := regexp.Compile(o.container)
	if err != nil {
		return nil, err
	}
	config.ContainerQuery = containerFilterRgx

	if o.tail != -1 {
		config.TailLines = &o.tail
	}

	// Do not use regex to filter pods. Instead, we provide the list of all control plane components and use
	// the label selector to filter logs.
	podFilterRgx, err := regexp.Compile("")
	if err != nil {
		return nil, err
	}
	config.PodQuery = podFilterRgx
	config.Since = o.sinceSeconds
	config.Timestamps = o.timestamps
	config.Namespace = controlPlaneNamespace

	return config, nil
}

func getControlPlaneComponentsAndContainers(pods *v1.PodList) ([]string, []string) {
	var controlPlaneComponents, containers []string
	for _, pod := range pods.Items {
		controlPlaneComponents = append(controlPlaneComponents, pod.Labels["linkerd.io/control-plane-component"])
		for _, container := range pod.Spec.Containers {
			containers = append(containers, container.Name)
		}
	}
	return controlPlaneComponents, containers
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

	podList, err := clientset.CoreV1().Pods(controlPlaneNamespace).List(meta_v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	components, containers := getControlPlaneComponentsAndContainers(podList)

	c, err := options.toSternConfig(components, containers)
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
		Short: "Tail logs from containers in the Linkerd control plane",
		Long:  `Tail logs from containers in the Linkerd control plane.`,
		Example: `  # Tail logs from all containers in the prometheus control plane component
  linkerd logs --control-plane-component prometheus

  # Tail logs from the linkerd-proxy container in the grafana control plane component
  linkerd logs --control-plane-component grafana --container linkerd-proxy

  # Tail logs from the linkerd-proxy container in the controller component beginning with the last two lines
  linkerd logs --control-plane-component controller --container linkerd-proxy --tail 2

  # Tail logs from the linkerd-proxy container in the controller component showing timestamps for each line
  linkerd logs --control-plane-component controller --container linkerd-proxy --timestamps
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			color.NoColor = options.noColor

			opts, err := newLogCmdConfig(options, kubeconfigPath, kubeContext)

			if err != nil {
				return err
			}

			return runLogOutput(opts)
		},
	}

	cmd.PersistentFlags().StringVarP(&options.container, "container", "c", options.container, "Tail logs from the specified container. Options are 'public-api', 'destination', 'tap', 'destination', 'prometheus', 'grafana' or 'linkerd-proxy'")
	cmd.PersistentFlags().StringVar(&options.controlPlaneComponent, "control-plane-component", options.controlPlaneComponent, "Tail logs from the specified control plane component. Default value (empty string) causes this command to tail logs from all resources marked with the 'linkerd.io/control-plane-component' label selector")
	cmd.PersistentFlags().BoolVarP(&options.noColor, "no-color", "n", options.noColor, "Disable colorized output") // needed until at least https://github.com/wercker/stern/issues/69 is resolved
	cmd.PersistentFlags().DurationVarP(&options.sinceSeconds, "since", "s", options.sinceSeconds, "Duration of how far back logs should be retrieved")
	cmd.PersistentFlags().Int64Var(&options.tail, "tail", options.tail, "Last number of log lines to show for a given container. -1 does not show previous log lines")
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

			newTail := stern.NewTail(a.Namespace, a.Pod, a.Container, tailOpts)
			if _, ok := tails[a.GetID()]; !ok {
				tails[a.GetID()] = newTail
			}
			newTail.Start(ctx, podInterface)
		}
	}()

	<-sigCh
	return nil
}
