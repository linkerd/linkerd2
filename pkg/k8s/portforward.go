package k8s

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
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
	localPort  int
	remotePort int
	stopCh     chan struct{}
	readyCh    chan struct{}
	config     *rest.Config
}

// NewPortForward returns an instance of the PortForward struct that can be used
// to establish a port-forward connection to a pod in the deployment that's
// specified by namespace and deployName. If localPort is 0, it will use a
// random ephemeral port.
func NewPortForward(
	configPath, kubeContext, namespace, deployName string,
	localPort, remotePort int,
) (*PortForward, error) {
	config, err := GetConfig(configPath, kubeContext)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	kubeAPI := &KubernetesAPI{Config: config}
	client, err := kubeAPI.NewClient()
	if err != nil {
		return nil, err
	}

	pods, err := kubeAPI.GetPodsByNamespace(client, namespace)
	if err != nil {
		return nil, err
	}

	podName := ""
	for _, pod := range pods {
		if pod.Status.Phase == v1.PodRunning {
			if strings.HasPrefix(pod.Name, deployName) {
				podName = pod.Name
				break
			}
		}
	}

	if podName == "" {
		return nil, fmt.Errorf("no running pods found for %s", deployName)
	}

	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("portforward")

	if localPort == 0 {
		localPort, err = getLocalPort()
		if err != nil {
			return nil, err
		}
	}

	return &PortForward{
		method:     "POST",
		url:        req.URL(),
		localPort:  localPort,
		remotePort: remotePort,
		stopCh:     make(chan struct{}, 1),
		readyCh:    make(chan struct{}),
		config:     config,
	}, nil
}

// Run creates and runs the port-forward connection.
func (pf *PortForward) Run() error {
	transport, upgrader, err := spdy.RoundTripperFor(pf.config)
	if err != nil {
		return err
	}

	ports := []string{fmt.Sprintf("%d:%d", pf.localPort, pf.remotePort)}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, pf.method, pf.url)

	fw, err := portforward.New(dialer, ports, pf.stopCh, pf.readyCh, ioutil.Discard, ioutil.Discard)
	if err != nil {
		return err
	}

	return fw.ForwardPorts()
}

// Ready returns a channel that will receive a message when the port-forward
// connection is ready. Clients should block and wait for the message before
// using the port-forward connection.
func (pf *PortForward) Ready() <-chan struct{} {
	return pf.readyCh
}

// Stop terminates the port-forward connection.
func (pf *PortForward) Stop() {
	close(pf.stopCh)
}

// URLFor returns the URL for the port-forward connection.
func (pf *PortForward) URLFor(path string) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", pf.localPort, path)
}

// getLocalPort binds to a free ephemeral port and returns the port number.
func getLocalPort() (int, error) {
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
