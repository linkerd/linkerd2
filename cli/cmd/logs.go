package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"text/template"
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
	container    string
	component    string
	sinceSeconds time.Duration
	tail         int64
	timestamps   bool
}

func newLogsOptions() *logsOptions {
	return &logsOptions{
		container:    "",
		component:    "",
		sinceSeconds: 48 * time.Hour,
		tail:         -1,
		timestamps:   false,
	}
}

func (o *logsOptions) toSternConfig(controlPlaneComponents, availableContainers []string) (*stern.Config, error) {
	config := &stern.Config{}

	if o.component == "" {
		config.LabelSelector = labels.Everything()
	} else {
		var podExists string
		for _, p := range controlPlaneComponents {
			if p == o.component {
				podExists = p
				break
			}
		}

		if podExists == "" {
			return nil, errors.New(fmt.Sprintf("control plane component [%s] does not exist. Must be one of %v", o.component, controlPlaneComponents))
		}
		selector, err := labels.Parse(fmt.Sprintf("linkerd.io/control-plane-component=%s", o.component))
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
			return nil, errors.New(fmt.Sprintf("container [%s] does not exist in control plane [%s]", o.container, controlPlaneNamespace))
		}
	}

	containerFilterRgx, err := regexp.Compile(o.container)
	if err != nil {
		return nil, err
	}
	config.ContainerQuery = containerFilterRgx

	containerState, err := stern.NewContainerState("running")
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

	if o.tail != -1 {
		config.TailLines = &o.tail
	}

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

func getControlPlaneComponentsAndContainers(pods *v1.PodList) (controlPlaneComponents []string, containers []string) {
	for _, pod := range pods.Items {
		controlPlaneComponents = append(controlPlaneComponents, pod.Labels["linkerd.io/control-plane-component"])
		for _, container := range pod.Spec.Containers {
			containers = append(containers, container.Name)
		}
	}
	return
}

func newLogCmdConfig(options *logsOptions, kubeconfigPath, kubeContext string) (*logCmdConfig, error) {
	// Check that we can call the Kubernetes API
	cliPublicAPIClient()
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
		Short: "Tail logs from containers in the linkerd control plane",
		Long:  `Tail logs from containers in the linkerd control plane`,
		Example: `  # Tail logs from all containers in the prometheus control plane component
  linkerd logs --component prometheus

  # Tail logs from the linkerd-proxy container in the grafana control plane component
  linkerd logs --component grafana --container linkerd-proxy

  # Tail logs from the linkerd-proxy container in the controller component with timestamps
  linkerd logs --component controller --container linkerd-proxy -t true
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := newLogCmdConfig(options, kubeconfigPath, kubeContext)

			if err != nil {
				return err
			}

			return runLogOutput(opts)
		},
	}

	cmd.PersistentFlags().StringVarP(&options.container, "container", "c", options.container, "Tail logs from the specified container. Options are 'public-api', 'proxy-api', 'tap', 'destination', 'prometheus', 'grafana' or 'linkerd-proxy'")
	cmd.PersistentFlags().StringVar(&options.component, "component", options.component, "Tail logs from the specified control plane component. Default value (empty string) causes this command to tail logs from all resources marked with the 'linkerd.io/control-plane-component' labelSelector")
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
