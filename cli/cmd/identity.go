package cmd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"github.com/grantae/certinfo"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
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
		pod:      "",
		selector: "",
	}
}

func newCmdIdentity() *cobra.Command {
	emitLog = false
	options := newIdentityOptions()

	cmd := &cobra.Command{
		Use:   "identity [flags] (PODS)",
		Short: "Display the certificate(s) of one or more selected pod(s)",
		Long: `Display the certificate(s) of one or more selected pod(s).
		
This command initiates a port-forward to a given pod or a set of pods and fetches the TLS certificate.
		`,
		Example: ` 
 # Get certificate from pod foo-bar in the default namespace.
 linkerd identity foo-bar
		
 # Get certificate from all pods with the label name=nginx
 linkerd identity -l name=nginx
		`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.namespace == "" {
				options.namespace = pkgcmd.GetDefaultNamespace(kubeconfigPath, kubeContext)
			}
			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			if len(args) == 0 && options.selector == "" {
				return fmt.Errorf("Provide the pod name argument or use the selector flag")
			}

			pods, err := getPods(cmd.Context(), k8sAPI, options.namespace, options.selector, args)
			if err != nil {
				return err
			}

			resultCerts := getCertificate(k8sAPI, pods, k8s.ProxyAdminPortName, emitLog)
			if len(resultCerts) == 0 {
				fmt.Print("Could not fetch Certificate. Ensure that the pod(s) are meshed by running `linkerd inject`\n")
				return nil
			}
			for i, resultCert := range resultCerts {
				fmt.Printf("\nPOD %s (%d of %d)\n\n", resultCert.pod, i+1, len(resultCerts))
				if resultCert.err != nil {
					fmt.Printf("\n%s\n", resultCert.err)
					return nil
				}
				for _, cert := range resultCert.Certificate {
					if cert.IsCA {
						continue
					}
					result, err := certinfo.CertificateText(cert)
					if err != nil {
						fmt.Printf("\n%s\n", err)
						return nil
					}
					fmt.Print(result)
				}
			}
			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of the pod")
	cmd.PersistentFlags().StringVarP(&options.selector, "selector", "l", options.selector, "Selector (label query) to filter on, supports ‘=’, ‘==’, and ‘!=’ ")
	return cmd
}

func getCertificate(k8sAPI *k8s.KubernetesAPI, pods []corev1.Pod, portName string, emitLog bool) []certificate {
	var certificates []certificate
	for _, pod := range pods {
		container, err := getContainerWithPort(pod, portName)
		if err != nil {
			certificates = append(certificates, certificate{
				pod: pod.GetName(),
				err: err,
			})
			return certificates
		}
		cert, err := getContainerCertificate(k8sAPI, pod, container, portName, emitLog)
		certificates = append(certificates, certificate{
			pod:         pod.GetName(),
			container:   container.Name,
			Certificate: cert,
			err:         err,
		})
	}
	return certificates
}

func getContainerWithPort(pod corev1.Pod, portName string) (corev1.Container, error) {
	var container corev1.Container
	if pod.Status.Phase != corev1.PodRunning {
		return container, fmt.Errorf("pod not running: %s", pod.GetName())
	}

	for _, c := range pod.Spec.Containers {
		if c.Name != k8s.ProxyContainerName {
			continue
		}
		for _, p := range c.Ports {
			if p.Name == portName {
				return c, nil
			}
		}
	}
	return container, fmt.Errorf("failed to find %s port in %s container for given pod spec", portName, k8s.ProxyContainerName)
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
	serverName, err := getServerName(pod, k8s.ProxyContainerName)
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

	serverName := fmt.Sprintf("%s.%s.serviceaccount.identity.%s.%s", podsa, podns, l5dns, l5dtrustdomain)
	return serverName, nil
}

func getPods(ctx context.Context, clientset kubernetes.Interface, namespace string, selector string, args []string) ([]corev1.Pod, error) {
	if len(args) > 0 {
		var pods []corev1.Pod
		for _, arg := range args {
			pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, arg, metav1.GetOptions{})
			if err != nil {
				return nil, err
			}
			pods = append(pods, *pod)
		}
		return pods, nil
	}

	podList, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, err
	}

	return podList.Items, nil
}
