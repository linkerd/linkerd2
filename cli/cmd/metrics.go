package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/linkerd/linkerd2/controller/api/util"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

type metricsOptions struct {
	namespace string
	pod       string
}

func newMetricsOptions() *metricsOptions {
	return &metricsOptions{
		pod: "",
	}
}

func newCmdMetrics() *cobra.Command {
	options := newMetricsOptions()

	cmd := &cobra.Command{
		Use:   "proxy-metrics [flags] (RESOURCE)",
		Short: "Fetch metrics directly from Linkerd proxies",
		Long: `Fetch metrics directly from Linkerd proxies.

  This command initiates a port-forward to a given pod or set of pods, and
  queries the /metrics endpoint on the Linkerd proxies.

  The RESOURCE argument specifies the target resource to query metrics for:
  (TYPE/NAME)

  Examples:
  * cronjob/my-cronjob
  * deploy/my-deploy
  * ds/my-daemonset
  * job/my-job
  * po/mypod1
  * rc/my-replication-controller
  * sts/my-statefulset

  Valid resource types include:
  * cronjobs
  * daemonsets
  * deployments
  * jobs
  * pods
  * replicasets
  * replicationcontrollers
  * statefulsets`,
		Example: `  # Get metrics from pod-foo-bar in the default namespace.
  linkerd diagnostics proxy-metrics po/pod-foo-bar

  # Get metrics from the web deployment in the emojivoto namespace.
  linkerd diagnostics proxy-metrics -n emojivoto deploy/web

  # Get metrics from the linkerd-destination pod in the linkerd namespace.
  linkerd diagnostics proxy-metrics -n linkerd $(
    kubectl --namespace linkerd get pod \
      --selector linkerd.io/control-plane-component=destination \
      --output name
  )`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.namespace == "" {
				options.namespace = pkgcmd.GetDefaultNamespace(kubeconfigPath, kubeContext)
			}
			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			pods, err := getPodsFor(cmd.Context(), k8sAPI, options.namespace, args[0])
			if err != nil {
				return err
			}

			results := getMetrics(k8sAPI, pods, k8s.ProxyAdminPortName, 30*time.Second, verbose)

			var buf bytes.Buffer
			for i, result := range results {
				content := fmt.Sprintf("#\n# POD %s (%d of %d)\n#\n", result.pod, i+1, len(results))
				if result.err == nil {
					content += string(result.metrics)
				} else {
					content += fmt.Sprintf("# ERROR %s\n", result.err)
				}
				buf.WriteString(content)
			}
			fmt.Printf("%s", buf.String())

			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of resource")

	return cmd
}

// getPodsFor takes a resource string, queries the Kubernetes API, and returns a
// list of pods belonging to that resource.
// This could move into `pkg/k8s` if becomes more generally useful.
func getPodsFor(ctx context.Context, clientset kubernetes.Interface, namespace string, resource string) ([]corev1.Pod, error) {
	// TODO: BuildResource parses a resource string (which we need), but returns
	// objects in Public API protobuf form for submission to the Public API
	// (which we don't need). Refactor this API to strictly support parsing
	// resource strings.
	res, err := util.BuildResource(namespace, resource)
	if err != nil {
		return nil, err
	}

	if res.GetName() == "" {
		return nil, errors.New("no resource name provided")
	}

	// special case if a single pod was specified
	if res.GetType() == k8s.Pod {
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, res.GetName(), metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return []corev1.Pod{*pod}, nil
	}

	var matchLabels map[string]string
	var ownerUID types.UID
	switch res.GetType() {
	case k8s.CronJob:
		jobs, err := clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}

		var pods []corev1.Pod
		for _, job := range jobs.Items {
			if isOwner(job.GetUID(), job.GetOwnerReferences()) {
				jobPods, err := getPodsFor(ctx, clientset, namespace, fmt.Sprintf("%s/%s", k8s.Job, job.GetName()))
				if err != nil {
					return nil, err
				}
				pods = append(pods, jobPods...)
			}
		}
		return pods, nil

	case k8s.DaemonSet:
		ds, err := clientset.AppsV1().DaemonSets(namespace).Get(ctx, res.GetName(), metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		matchLabels = ds.Spec.Selector.MatchLabels
		ownerUID = ds.GetUID()

	case k8s.Deployment:
		deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, res.GetName(), metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		matchLabels = deployment.Spec.Selector.MatchLabels
		ownerUID = deployment.GetUID()

		replicaSets, err := clientset.AppsV1().ReplicaSets(namespace).List(
			ctx,
			metav1.ListOptions{
				LabelSelector: labels.Set(matchLabels).AsSelector().String(),
			},
		)
		if err != nil {
			return nil, err
		}

		var pods []corev1.Pod
		for _, rs := range replicaSets.Items {
			if isOwner(ownerUID, rs.GetOwnerReferences()) {
				podsRS, err := getPodsFor(ctx, clientset, namespace, fmt.Sprintf("%s/%s", k8s.ReplicaSet, rs.GetName()))
				if err != nil {
					return nil, err
				}
				pods = append(pods, podsRS...)
			}
		}
		return pods, nil

	case k8s.Job:
		job, err := clientset.BatchV1().Jobs(namespace).Get(ctx, res.GetName(), metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		matchLabels = job.Spec.Selector.MatchLabels
		ownerUID = job.GetUID()

	case k8s.ReplicaSet:
		rs, err := clientset.AppsV1().ReplicaSets(namespace).Get(ctx, res.GetName(), metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		matchLabels = rs.Spec.Selector.MatchLabels
		ownerUID = rs.GetUID()

	case k8s.ReplicationController:
		rc, err := clientset.CoreV1().ReplicationControllers(namespace).Get(ctx, res.GetName(), metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		matchLabels = rc.Spec.Selector
		ownerUID = rc.GetUID()

	case k8s.StatefulSet:
		ss, err := clientset.AppsV1().StatefulSets(namespace).Get(ctx, res.GetName(), metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		matchLabels = ss.Spec.Selector.MatchLabels
		ownerUID = ss.GetUID()

	default:
		return nil, fmt.Errorf("unsupported resource type: %s", res.GetType())
	}

	podList, err := clientset.
		CoreV1().
		Pods(namespace).
		List(
			ctx,
			metav1.ListOptions{
				LabelSelector: labels.Set(matchLabels).AsSelector().String(),
			},
		)
	if err != nil {
		return nil, err
	}

	if ownerUID == "" {
		return podList.Items, nil
	}

	pods := []corev1.Pod{}
	for _, pod := range podList.Items {
		if isOwner(ownerUID, pod.GetOwnerReferences()) {
			pods = append(pods, pod)
		}
	}

	return pods, nil
}

func isOwner(u types.UID, ownerRefs []metav1.OwnerReference) bool {
	for _, or := range ownerRefs {
		if u == or.UID {
			return true
		}
	}
	return false
}
