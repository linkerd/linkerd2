package cmd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/linkerd/linkerd2/controller/api/util"
	"github.com/linkerd/linkerd2/pkg/k8s"
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
		linkerd identity pod/foo-bar

		#Get certificates from all pods in emojivoto namespace
		linkerd identity -n emojivoto
		
		#Get certificate from all pods with name=nginx
		linkerd identity -l name=nginx
		`,
		RunE: func(cmd *cobra.Command, args []string) error {
			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}
			pods, err := getPods(cmd.Context(), k8sAPI, options.namespace, options.selector, args)
			if err != nil {
				return err
			}
			results := getCertificate(k8sAPI, pods, k8s.ProxyAdminPortName, 30*time.Second, emitLog)

			for i, result := range results {
				fmt.Printf("\n POD %s (%d of %d)\n\n", result.pod, i+1, len(results))
				if result.err == nil {
					fmt.Println("Version: ", result.Certificate[0].Version)
					fmt.Println("Serial Number: ", result.Certificate[0].SerialNumber)
					fmt.Println("Signature Algorithm: ", result.Certificate[0].SignatureAlgorithm)
					fmt.Println("Validity")
					fmt.Println("\t Not Before: ", result.Certificate[0].NotBefore.Format(time.RFC3339))
					fmt.Println("\t Not After: ", result.Certificate[0].NotAfter.Format(time.RFC3339))
					fmt.Println("Subject: ", result.Certificate[0].Subject)
					fmt.Println("Issuer:  ", result.Certificate[0].Issuer)
					fmt.Println("Subject Public Key Info:")
					fmt.Println("\tPublic Key Algorithm: ", result.Certificate[0].PublicKeyAlgorithm)
					fmt.Println("Signature: \n", result.Certificate[0].Signature)
					resultCerticate := tls1.EncodeCertificatesPEM(result.Certificate[0])

					fmt.Printf("\n%s", resultCerticate)
				} else {
					fmt.Printf("\n%s\n", result.err)
				}
			}
			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace to use for --proxy versions (default: all namespaces)")
	cmd.PersistentFlags().StringVarP(&options.selector, "selector", "l", options.selector, "Selector (label query) to filter on, supports ‘=’, ‘==’, and ‘!=’ ")
	return cmd
}

// getCertificate fetches the certificates for all the pods
func getCertificate(k8sAPI *k8s.KubernetesAPI, pods []corev1.Pod, portName string, waitingTime time.Duration, emitLog bool) []certificate {
	var certificates []certificate
	resultChan := make(chan certificate)
	var activeRoutines int32
	for _, pod := range pods {
		atomic.AddInt32(&activeRoutines, 1)
		go func(p corev1.Pod) {
			defer atomic.AddInt32(&activeRoutines, -1)
			containers, err := getContainersWithPort(p, portName)
			if err != nil {
				resultChan <- certificate{
					pod: p.GetName(),
					err: err,
				}
				return
			}

			for _, c := range containers {
				cert, err := getContainerCertificate(k8sAPI, p, c, portName, emitLog)
				resultChan <- certificate{
					pod:         p.GetName(),
					container:   c.Name,
					Certificate: cert,
					err:         err,
				}
			}
		}(pod)
	}

	for {
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
	rootPEM, err := getTrustAnchor(pod, "linkerd-proxy")
	if err != nil {
		return nil, err
	}

	re := regexp.MustCompile("[0-9]+")
	res := re.FindString(url)

	connURL := "localhost:" + res

	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM([]byte(rootPEM))

	conn, err := tls.Dial("tcp", connURL, &tls.Config{
		RootCAs:    roots,
		ServerName: serverName,
	})

	if err != nil {
		return nil, err
	}

	cert := conn.ConnectionState().PeerCertificates
	return cert, nil
}

func getTrustAnchor(pod corev1.Pod, containerName string) (string, error) {
	if pod.Status.Phase != corev1.PodRunning {
		return "", fmt.Errorf("pod not running: %s", pod.GetName())
	}

	var trustAnchor string
	for _, c := range pod.Spec.Containers {
		if c.Name == containerName {
			for _, env := range c.Env {
				if env.Name == "LINKERD2_PROXY_IDENTITY_TRUST_ANCHORS" {
					trustAnchor = env.Value
				}
			}
		}
	}
	return trustAnchor, nil
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
		res, err := util.BuildResource(namespace, arg[0])
		if err != nil {
			return nil, err
		}
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, res.GetName(), metav1.GetOptions{})
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
