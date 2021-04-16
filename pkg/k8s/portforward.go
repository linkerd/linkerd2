package k8s

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

// PortForward provides a port-forward connection into a Kubernetes cluster.
type PortForward struct {
	method     string
	url        *url.URL
	host       string
	namespace  string
	podName    string
	localPort  int
	remotePort int
	emitLogs   bool
	stopCh     chan struct{}
	readyCh    chan struct{}
	config     *rest.Config
}

// NewContainerMetricsForward returns an instance of the PortForward struct that can
// be used to establish a port-forward connection to a containers metrics
// endpoint, specified by namespace, pod, container and portName.
func NewContainerMetricsForward(
	k8sAPI *KubernetesAPI,
	pod corev1.Pod,
	container corev1.Container,
	emitLogs bool,
	portName string,
) (*PortForward, error) {
	var port corev1.ContainerPort
	for _, p := range container.Ports {
		if p.Name == portName {
			port = p
			break
		}
	}
	if port.Name != portName {
		return nil, fmt.Errorf("no %s port found for container %s/%s", portName, pod.GetName(), container.Name)
	}

	return newPortForward(k8sAPI, pod.GetNamespace(), pod.GetName(), "localhost", 0, int(port.ContainerPort), emitLogs)
}

// NewPortForward returns an instance of the PortForward struct that can be used
// to establish a port-forward connection to a pod in the deployment that's
// specified by namespace and deployName. If localPort is 0, it will use a
// random ephemeral port.
// Note that the connection remains open for the life of the process, as this
// function is typically called by the CLI. Care should be taken if called from
// control plane code.
func NewPortForward(
	ctx context.Context,
	k8sAPI *KubernetesAPI,
	namespace, deployName string,
	host string, localPort, remotePort int,
	emitLogs bool,
) (*PortForward, error) {
	timeoutSeconds := int64(30)
	podList, err := k8sAPI.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{TimeoutSeconds: &timeoutSeconds})
	if err != nil {
		return nil, err
	}

	podName := ""
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodRunning {
			grandparent, err := getDeploymentForPod(ctx, k8sAPI, pod)
			if err != nil {
				log.Warnf("Failed to get deploy for pod [%s]: %s", pod.Name, err)
				continue
			}
			if grandparent == deployName {
				podName = pod.Name
				break
			}
		}
	}

	if podName == "" {
		return nil, fmt.Errorf("no running pods found for %s", deployName)
	}

	return newPortForward(k8sAPI, namespace, podName, host, localPort, remotePort, emitLogs)
}

func getDeploymentForPod(ctx context.Context, k8sAPI *KubernetesAPI, pod corev1.Pod) (string, error) {
	parents := pod.GetOwnerReferences()
	if len(parents) != 1 {
		return "", nil
	}
	rs, err := k8sAPI.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, parents[0].Name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	grandparents := rs.GetOwnerReferences()
	if len(grandparents) != 1 {
		return "", nil
	}
	return grandparents[0].Name, nil
}

func newPortForward(
	k8sAPI *KubernetesAPI,
	namespace, podName string,
	host string, localPort, remotePort int,
	emitLogs bool,
) (*PortForward, error) {

	req := k8sAPI.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("portforward")

	var err error
	if localPort == 0 {
		localPort, err = getEphemeralPort()
		if err != nil {
			return nil, err
		}
	}

	return &PortForward{
		method:     "POST",
		url:        req.URL(),
		host:       host,
		namespace:  namespace,
		podName:    podName,
		localPort:  localPort,
		remotePort: remotePort,
		emitLogs:   emitLogs,
		stopCh:     make(chan struct{}, 1),
		readyCh:    make(chan struct{}),
		config:     k8sAPI.Config,
	}, nil
}

// run creates and runs the port-forward connection.
// When the connection is established it blocks until Stop() is called.
func (pf *PortForward) run() error {
	transport, upgrader, err := spdy.RoundTripperFor(pf.config)
	if err != nil {
		return err
	}

	out := ioutil.Discard
	errOut := ioutil.Discard
	if pf.emitLogs {
		out = os.Stdout
		errOut = os.Stderr
	}

	ports := []string{fmt.Sprintf("%d:%d", pf.localPort, pf.remotePort)}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, pf.method, pf.url)

	fw, err := portforward.NewOnAddresses(dialer, []string{pf.host}, ports, pf.stopCh, pf.readyCh, out, errOut)
	if err != nil {
		return err
	}

	err = fw.ForwardPorts()
	if err != nil {
		err = fmt.Errorf("%s for %s/%s", err, pf.namespace, pf.podName)
		return err
	}
	return nil
}

// Init creates and runs a port-forward connection.
// This function blocks until the connection is established, in which case it returns nil.
// It's the caller's responsibility to call Stop() to finish the connection.
func (pf *PortForward) Init() error {
	log.Debugf("Starting port forward to %s %d:%d", pf.url, pf.localPort, pf.remotePort)

	failure := make(chan error)

	go func() {
		if err := pf.run(); err != nil {
			failure <- err
		}
	}()

	// The `select` statement below depends on one of two outcomes from `pf.run()`:
	// 1) Succeed and block, causing a receive on `<-pf.readyCh`
	// 2) Return an err, causing a receive `<-failure`
	select {
	case <-pf.readyCh:
		log.Debug("Port forward initialised")
	case err := <-failure:
		log.Debugf("Port forward failed: %v", err)
		return err
	}

	return nil
}

// Stop terminates the port-forward connection.
// It is the caller's responsibility to call Stop even in case of errors
func (pf *PortForward) Stop() {
	close(pf.stopCh)
}

// GetStop returns the stopCh.
// Receiving on stopCh will block until the port forwarding stops.
func (pf *PortForward) GetStop() <-chan struct{} {
	return pf.stopCh
}

// URLFor returns the URL for the port-forward connection.
func (pf *PortForward) URLFor(path string) string {
	return fmt.Sprintf("http://%s:%d%s", pf.host, pf.localPort, path)
}

// AddressAndPort returns the address and port for the port-forward connection.
func (pf *PortForward) AddressAndPort() string {
	return fmt.Sprintf("%s:%d", pf.host, pf.localPort)
}

// getEphemeralPort selects a port for the port-forwarding. It binds to a free
// ephemeral port and returns the port number.
func getEphemeralPort() (int, error) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}

	defer ln.Close()

	// get port
	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("invalid listen address: %s", ln.Addr())
	}

	return tcpAddr.Port, nil
}
