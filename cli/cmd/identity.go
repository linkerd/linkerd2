package cmd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"github.com/grantae/certinfo"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/k8s/resource"
	tls1 "github.com/linkerd/linkerd2/pkg/tls"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var emitLog bool

type certificate struct {
	pod         string
	container   string
	Certificate []*x509.Certificate
	err         error
}

type identityOptions struct {
	pod       string
	namespace string
	selector  string
}

func newIdentityOptions() *identityOptions {
	return &identityOptions{
		pod:       "",
		namespace: "",
		selector:  "",
	}
}

func newCmdIdentity() *cobra.Command {
	emitLog = false
	options := newIdentityOptions()

	cmd := &cobra.Command{
		Use:   "identity [flags] (PODS)",
		Short: "Display the certificate(s) of one or more selected pod(s)",
		Long: `Display the certificate(s) of one or more selected pod(s).
		
		This command initiates a port-forward to a given pod or a set of pods and
		fetches the tls certificate.
		`,
		Example: ` 
		#Get certificate from pod foo-bar in the default namespace.
		linkerd identity foo-bar
		
		#Get certificate from all pods with name=nginx
		linkerd identity -l name=nginx
		`,
		RunE: func(cmd *cobra.Command, args []string) error {
			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			resources, err := resource.FetchKubernetesResources(cmd.Context(), k8sAPI, metav1.ListOptions{LabelSelector: k8s.ControllerNSLabel})
			if err != nil {
				return err
			}
			// check if linkerd control plane exists
			if len(resources) == 0 {
				return fmt.Errorf("Cannot find Linkerd\nValidate the install with: linkerd check")
			}
			if len(args) == 0 && options.selector == "" {
				return fmt.Errorf("Provide the pod name argument or use the selector flag")
			}

			pods, err := getPods(cmd.Context(), k8sAPI, options.namespace, options.selector, args)
			if err != nil {
				return err
			}

			resultCerts := getCertificate(k8sAPI, pods, k8s.ProxyAdminPortName, emitLog)
			for _, resultCert := range resultCerts {
				if resultCert.err != nil {
					fmt.Printf("\n%s", resultCert.err)
					return nil
				}
				certChain := resultCert.Certificate
				cert := certChain[len(certChain)-1]
				result, err := certinfo.CertificateText(cert)
				if err != nil {
					fmt.Printf("\n%s", err)
					return nil
				}
				fmt.Print(result)
				fmt.Println("pem: |")
				fmt.Print(tls1.EncodeCertificatesPEM(cert))
			}
			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace to use for --proxy versions (default: all namespaces)")
	cmd.PersistentFlags().StringVarP(&options.selector, "selector", "l", options.selector, "Selector (label query) to filter on, supports ‘=’, ‘==’, and ‘!=’ ")
	return cmd
}

func getCertificate(k8sAPI *k8s.KubernetesAPI, pods []corev1.Pod, portName string, emitLog bool) []certificate {
	var certificates []certificate
	for _, pod := range pods {
		containers, err := getContainersWithPort(pod, portName)
		if err != nil {
			certificates = append(certificates, certificate{
				pod: pod.GetName(),
				err: err,
			})
			return certificates
		}

		for _, c := range containers {
			cert, err := getContainerCertificate(k8sAPI, pod, c, portName, emitLog)
			certificates = append(certificates, certificate{
				pod:         pod.GetName(),
				container:   c.Name,
				Certificate: cert,
				err:         err,
			})
		}
	}
	return certificates
}

func getContainersWithPort(pod corev1.Pod, portName string) ([]corev1.Container, error) {
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
	serverName, err := getServerName(pod, "linkerd-proxy")
	if err != nil {
		return nil, err
	}
	connURL := strings.Trim(url, "http://")
	conn, err := tls.Dial("tcp", connURL, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         serverName,
	})

	if err != nil {
		return nil, err
	}

	cert := conn.ConnectionState().PeerCertificates
	return cert, nil
}

func getServerName(pod corev1.Pod, containerName string) (string, error) {
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

func getPods(ctx context.Context, clientset kubernetes.Interface, namespace string, selector string, arg []string) ([]corev1.Pod, error) {
	if len(arg) > 0 {
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, arg[0], metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return []corev1.Pod{*pod}, nil
	}

	podList, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, err
	}

	return podList.Items, nil
}
