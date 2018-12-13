package k8s

import (
	"fmt"
	"net"
	"net/url"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"
	"k8s.io/kubernetes/pkg/kubectl/proxy"

	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

type KubernetesProxy struct {
	listener net.Listener
	server   *proxy.Server
}

// NewProxy returns a new KubernetesProxy object and starts listening on a
// network address.
func NewProxy(configPath, kubeContext string, proxyPort int) (*KubernetesProxy, error) {
	config, err := GetConfig(configPath, kubeContext)
	if err != nil {
		return nil, fmt.Errorf("error configuring Kubernetes API client: %v", err)
	}

	server, err := proxyCreate(config)
	if err != nil {
		return nil, fmt.Errorf("Failed to create proxy: %+v", err)
	}

	listener, err := proxyListen(server, proxyPort)
	if err != nil {
		return nil, fmt.Errorf("Failed to listen with proxy: %+v", err)
	}

	return &KubernetesProxy{
		listener: listener,
		server:   server,
	}, nil
}

// Run starts proxying a connection to Kubernetes, and blocks until the process
// exits.
func (kp *KubernetesProxy) Run() error {
	// blocks until process is killed
	err := proxyServe(kp.server, kp.listener)
	if err != nil {
		return fmt.Errorf("Failed to serve with proxy: %+v", err)
	}

	return nil
}

// URLFor generates a URL based on the configured KubernetesProxy.
func (kp *KubernetesProxy) URLFor(namespace string, extraPathStartingWithSlash string) (*url.URL, error) {
	schemeHostAndPort := fmt.Sprintf("http://127.0.0.1:%d", kp.listener.Addr().(*net.TCPAddr).Port)
	return generateKubernetesAPIBaseURLFor(schemeHostAndPort, namespace, extraPathStartingWithSlash)
}

func proxyCreate(config *rest.Config) (*proxy.Server, error) {
	filter := &proxy.FilterServer{
		AcceptPaths:   proxy.MakeRegexpArrayOrDie(proxy.DefaultPathAcceptRE),
		RejectPaths:   proxy.MakeRegexpArrayOrDie(proxy.DefaultPathRejectRE),
		AcceptHosts:   proxy.MakeRegexpArrayOrDie(proxy.DefaultHostAcceptRE),
		RejectMethods: proxy.MakeRegexpArrayOrDie(proxy.DefaultMethodRejectRE),
	}
	server, err := proxy.NewServer("", "/", "/static/", filter, config)
	if err != nil {
		return nil, fmt.Errorf("Failed to create proxy server: %+v", err)
	}

	return server, nil
}

func proxyListen(server *proxy.Server, proxyPort int) (net.Listener, error) {
	listener, err := server.Listen("127.0.0.1", proxyPort)
	if err != nil {
		return nil, fmt.Errorf("Failed to listen via proxy server: %+v", err)
	}

	return listener, nil
}

func proxyServe(server *proxy.Server, listener net.Listener) error {
	log.Infof("Starting to serve on %s", listener.Addr().String())

	// blocks until process is killed
	err := server.ServeOnListener(listener)
	if err != nil {
		return fmt.Errorf("Failed to serve via proxy server: %+v", err)
	}

	return nil
}
