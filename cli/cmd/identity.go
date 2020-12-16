package cmd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/linkerd/linkerd2/controller/api/util"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var emitLog bool

type Certificates struct {
	pod         string
	container   string
	Certificate []*x509.Certificate
	err         error
}

type identityOptions struct {
	pod       string
	namespace string
}

func newIdentityOptions() *identityOptions {
	return &identityOptions{
		pod:       "",
		namespace: "",
	}
}

func newCmdIdentity() *cobra.Command {
	emitLog = false
	options := newIdentityOptions()

	cmd := &cobra.Command{
		Use:   "identity [name]",
		Short: "Display the certificate(s) of one or more selected pod(s)",
		Long: `Display the certificate(s) of one or more selected pod(s).
		
		This command initiates a port-forward to a given pod or a set of pods, and
		fetches the tls certificate.
		`,
		Example: ` #Get certificate from pod foo-bar in the default namespace.
		linkerd identity foo-bar
		
		#Get certificate from all pods in the default namespace
		linkerd identity
		
		#Get certificate from the web deployment in the emojivoto namespace
		linkerd identity deploy/web -n emojivoto`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}
			pods, err := getPods(cmd.Context(), k8sAPI, options.namespace, args[0])
			if err != nil {
				//fmt.Print("")
				return err
			}
			// fmt.Printf("\nPod length: %d", len(pods))
			results := getCertificate(k8sAPI, pods, k8s.ProxyAdminPortName, 30*time.Second, emitLog)

			for _, result := range results {
				// fmt.Printf("\nDEBUG")
				// content := fmt.Sprintf("#\n# POD %s (%d of %d)\n#\n", result.pod, i+1, len(results))
				if result.err == nil {
					fmt.Printf("\n%+v\n", result.Certificate)
				} else {
					fmt.Printf("\n%s\n", result.err)
				}
			}
			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace to use for --proxy versions (default: all namespaces)")

	return cmd
}

func getCertificate(k8sAPI *k8s.KubernetesAPI, pods []corev1.Pod, portName string, waitingTime time.Duration, emitLog bool) []Certificates {
	//fmt.Printf("\nInside getCertificate")
	var certificates []Certificates
	resultChan := make(chan Certificates)
	var activeRoutines int32
	for _, pod := range pods {
		//fmt.Printf("in loop")
		atomic.AddInt32(&activeRoutines, 1)
		go func(p corev1.Pod) {
			defer atomic.AddInt32(&activeRoutines, -1)
			containers, err := getContainersWithPort(p, portName)
			if err != nil {
				resultChan <- Certificates{
					pod: p.GetName(),
					err: err,
				}
				return
			}

			for _, c := range containers {
				cert, err := getContainerCertificate(k8sAPI, p, c, portName, emitLog)

				resultChan <- Certificates{
					pod:         p.GetName(),
					container:   c.Name,
					Certificate: cert,
					err:         err,
				}
			}
		}(pod)
	}

	for {
		//fmt.Printf("stuck")
		select {
		case cert := <-resultChan:
			certificates = append(certificates, cert)
		case <-time.After(waitingTime):
			break
		}
		if atomic.LoadInt32(&activeRoutines) == 0 {
			break
		}
	}
	return certificates
}

func getContainersWithPort(pod corev1.Pod, portName string) ([]corev1.Container, error) {
	// fmt.Printf("\n Inside getContainersWithPort")
	if pod.Status.Phase != corev1.PodRunning {
		return nil, fmt.Errorf("pod not running: %s", pod.GetName())
	}
	var containers []corev1.Container

	for _, c := range pod.Spec.Containers {
		for _, p := range c.Ports {
			if p.Name == portName {
				containers = append(containers, c)
			}
		}
	}
	return containers, nil
}

func getContainerCertificate(k8sAPI *k8s.KubernetesAPI, pod corev1.Pod, container corev1.Container, portName string, emitLog bool) ([]*x509.Certificate, error) {
	// fmt.Printf("\nInside getContainerCertificate")
	portForward, err := k8s.NewContainerMetricsForward(k8sAPI, pod, container, emitLog, portName)
	if err != nil {
		return nil, err
	}

	defer portForward.Stop()
	if err = portForward.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running port-forward: %s", err)
		return nil, err
	}

	certURL := portForward.URLFor("")
	return getCertResponse(certURL, pod)
}

func getCertResponse(url string, pod corev1.Pod) ([]*x509.Certificate, error) {
	// can we get the container env from pod spec?
	serverName, err := getServerName(pod, "linkerd-proxy")
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile("[0-9]+")
	res := re.FindString(url)
	// fmt.Printf("\nPort here: %s", res)
	connURL := "localhost:" + res
	// fmt.Printf("\n URL %s", connURL)
	conn, err := net.Dial("tcp", connURL)
	if err != nil {
		return nil, err
	}

	fmt.Printf("\nserver name: %s", serverName)

	client := tls.Client(conn, &tls.Config{
		// get server name from LINKERD2_PROXY_IDENTITY_LOCAL_NAME env var in the proxy container
		ServerName: serverName,
	})
	defer client.Close()

	if err := client.Handshake(); err != nil {
		return nil, err
	}

	cert := client.ConnectionState().PeerCertificates
	return cert, nil
}

func getServerName(pod corev1.Pod, containerName string) (string, error) {
	// we need to form the server name using the following env var from the proxy container:
	// - _pod_sa: this refers to the service account name - (serviceAccountName in the pod spec)
	// - _pod_ns: (namespace in pod spec)
	// - _l5d_ns
	// - _l5d_trustdomain
	// $(_pod_sa).$(_pod_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)
	if pod.Status.Phase != corev1.PodRunning {
		return "", fmt.Errorf("pod not running: %s", pod.GetName())
	}

	var l5dns string
	var l5dtrustdomain string
	podsa := pod.Spec.ServiceAccountName
	podns := pod.ObjectMeta.Namespace
	for _, c := range pod.Spec.Containers {
		if c.Name == containerName {
			for _, env := range c.Env {
				if env.Name == "_l5d_ns" {
					l5dns = env.Value
				}
				if env.Name == "_l5d_trustdomain" {
					l5dtrustdomain = env.Value
				}
			}
		}
	}

	serverName := podsa + "." + podns + ".serviceaccount.identity." + l5dns + "." + l5dtrustdomain

	return serverName, nil
}

func getPods(ctx context.Context, clientset kubernetes.Interface, namespace string, arg string) ([]corev1.Pod, error) {

	// var podList *corev1.PodList
	res, err := util.BuildResource(namespace, arg)
	if err != nil {
		return nil, err
	}

	// // get all pods in the current namespace
	// if res.GetName() == "" {
	// 	podList, err = clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// }

	// check if the resource is actually a pod or not
	if res.GetType() != k8s.Pod {
		return nil, fmt.Errorf("could not find pod %s", res.GetType())
	}

	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, res.GetName(), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return []corev1.Pod{*pod}, nil
}
