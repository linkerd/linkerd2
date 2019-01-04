package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	"github.com/ttacon/chalk"
	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type logFilter struct {
	targetPod           v1.Pod
	targetContainerName string
}

type logCmdOpts struct {
	kubeAPI          *k8s.KubernetesAPI
	k8sClient        *http.Client
	controlPlanePods *v1.PodList
	clientset        *kubernetes.Clientset
	*logFilter
}

type ColorPicker struct {
	m               map[string]chalk.Color
	mu              sync.Mutex
	availableColors []chalk.Color
	lastUsedColor   int
}

func (c *ColorPicker) pick(id string) chalk.Color {
	c.mu.Lock()
	defer c.mu.Unlock()
	if color, ok := c.m[id]; !ok {
		if c.lastUsedColor > len(c.availableColors)-1 {
			c.lastUsedColor = 1
		}
		newColor := c.availableColors[c.lastUsedColor]
		c.m[id] = newColor
		c.lastUsedColor += 1
		return newColor
	} else {
		return color
	}

}

func newColorPicker() *ColorPicker {
	return &ColorPicker{
		m: map[string]chalk.Color{},
		availableColors: []chalk.Color{
			chalk.Yellow,
			chalk.Red,
			chalk.Cyan,
			chalk.Green,
			chalk.Magenta,
			chalk.White,
		},
	}
}

func newLogOptions(args []string, containerFilter, kubeconfigPath, kubeContext string) (*logCmdOpts, error) {
	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext)
	if err != nil {
		return nil, err
	}

	client, err := kubeAPI.NewClient()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(kubeAPI.Config)
	if err != nil {
		return nil, err
	}

	controlPlanePods, err := clientset.
		CoreV1().
		Pods(controlPlaneNamespace).
		List(meta_v1.ListOptions{})

	filterOpts, err := validateArgs(args, controlPlanePods, containerFilter)
	if err != nil {
		return nil, err
	}

	return &logCmdOpts{
		kubeAPI,
		client,
		controlPlanePods,
		clientset,
		filterOpts,
	}, nil
}

func newCmdLogs() *cobra.Command {

	var containerFilter string

	cmd := &cobra.Command{
		Use:   "logs (COMPONENT) [flags]",
		Short: "Prints logs for controller components",
		Long:  `Prints logs for controller components`,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := newLogOptions(args, containerFilter, kubeconfigPath, kubeContext)

			if err != nil {
				return err
			}

			return runLogOutput(os.Stdout, opts)
		},
	}

	cmd.PersistentFlags().StringVarP(&containerFilter, "container", "c", containerFilter, "Filters log lines by provided container name")

	return cmd
}

func (l *logCmdOpts) followLogs(pod, container string, logLineCh chan<- string, colorPicker *ColorPicker) {
	stream, err := l.clientset.
		CoreV1().
		Pods(controlPlaneNamespace).
		GetLogs(pod, &v1.PodLogOptions{
			Container: container,
			Follow:    true,
		}).
		Stream()

	if err != nil {
		return
	}

	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	loglineId := fmt.Sprintf("[%s %s]", pod, container)

	for scanner.Scan() {
		logLineCh <- fmt.Sprintf("%s %s\n", colorPicker.pick(loglineId).Color(loglineId), scanner.Text())
	}

}

func runLogOutput(writer io.Writer, opts *logCmdOpts) error {

	logLineCh := make(chan string)
	colorPicker := newColorPicker()

	if opts.targetPod.Name == "" && opts.targetContainerName == "" {
		for _, pod := range opts.controlPlanePods.Items {
			for _, container := range pod.Spec.Containers {
				go opts.followLogs(pod.Name, container.Name, logLineCh, colorPicker)
			}
		}
	} else if opts.targetPod.Name != "" && opts.targetContainerName == "" {
		for _, container := range opts.targetPod.Spec.Containers {
			go opts.followLogs(opts.targetPod.Name, container.Name, logLineCh, colorPicker)
		}
	} else if opts.targetPod.Name != "" && opts.targetContainerName != "" {
		go opts.followLogs(opts.targetPod.Name, opts.targetContainerName, logLineCh, colorPicker)
	}

	for {
		select {
		case line := <-logLineCh:
			_, err := fmt.Fprint(writer, line)
			if err != nil {
				return err
			}
		}
	}
}

// validateArgs returns podWithContainer if args and container name matches
// a valid pod and a valid container within that pod
func validateArgs(args []string, pods *v1.PodList, containerName string) (*logFilter, error) {
	var podName string
	if len(args) == 1 {
		podName = args[0]
	}

	if pods == nil {
		return nil, errors.New("no pods to filter logs from")
	}

	for _, pod := range pods.Items {
		for _, ref := range pod.OwnerReferences{
			println(ref.Name)
		}

		if pod.Name == podName {
			return &logFilter{pod, containerName}, nil
		}
		for _, container := range pod.Spec.Containers {
			if containerName != "" && containerName == container.Name {
				return &logFilter{pod, containerName}, nil
			}
		}
	}

	// If we have exhausted the entire pod list and haven't found the container we are looking for
	// return as error as that container does not exist in the control plane.
	if containerName != "" {
		return nil, errors.New(fmt.Sprintf("[%s] is not a valid container in pod [%s]", containerName, podName))
	}

	return &logFilter{}, nil
}
