package servicemirror

import (
	"flag"
	"fmt"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/flags"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	mirrorSecretType = "mirror.linkerd.io/kubeconfig"
	mirrorServiceLabel = "linkerd.io/mirror-service"
	configKeyName = "config"
)

type RemoteClusterGatewayService  struct {
	name string
	namespace string
	gatewayAddress string
	gatewayPort string
}

type RemoteClusterServiceWatcher struct {
	clusterName string
	apiClient *k8s.API
	localApiClient *k8s.API
	gatewayService *RemoteClusterGatewayService
}

func NewRemoteClusterServiceWatcher(localApi *k8s.API, cfg *rest.Config, clusterName string) (*RemoteClusterServiceWatcher, error) {
	remoteApi, err := k8s.InitializeAPIForConfig(cfg, k8s.Svc)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize remote api for cluster %s: %s", clusterName, err)
	}
	remoteApi.Sync()
	return &RemoteClusterServiceWatcher{ clusterName: clusterName,localApiClient:localApi, apiClient:remoteApi}, nil
}


func (sw *RemoteClusterServiceWatcher) mirrorRemoteService(obj interface{}) {
	// we need to create a service and an endpoint
	svc := obj.(*corev1.Service)

	lbls := map[string]string{mirrorServiceLabel: "true"}

	_, err := sw.localApiClient.Client.CoreV1().Namespaces().Get(svc.Namespace, metav1.GetOptions{})

	if err != nil {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      lbls,
				Name:        svc.Namespace,
			},
		}
		_, err = sw.localApiClient.Client.CoreV1().Namespaces().Create(ns)

		if err != nil {
			log.Errorf("could not create mirror namespace: %s", err)
		}
	}



	serviceToCreate := &corev1.Service {
		ObjectMeta: metav1.ObjectMeta {
			Name: svc.Name,
			Namespace: svc.Namespace,
			Labels: lbls,
		},
		Spec:       corev1.ServiceSpec {
			Ports:                    []corev1.ServicePort {{
				Protocol:   corev1.ProtocolTCP,
				Port:       80,
				TargetPort: intstr.IntOrString{IntVal: 30080},
			}},

		},
	}

	s, err := sw.localApiClient.Client.CoreV1().Services(svc.Namespace).Create(serviceToCreate)
	if err != nil {
		log.Errorf("could not create mirror service: %s", err)
	} else {
		log.Infof("Created mirror service %s", s.Name)
	}


	// now we need to create an endpoint....

		endpoints := &corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svc.Name,
				Namespace: svc.Namespace,
			},
			Subsets: []corev1.EndpointSubset{
				{
					Addresses: []corev1.EndpointAddress{
						{
							IP: sw.gatewayService.gatewayAddress,
						},
					},
					Ports: []corev1.EndpointPort{{
						Port:     30080,
					}},
				},
			},
		}

		endp, err := sw.localApiClient.Client.CoreV1().Endpoints(svc.Namespace).Create(endpoints)
		if err != nil {
			log.Errorf("could not create mirror service: %s", err)
		} else {
			log.Infof("Created mirror service %s", endp.Name)
		}

}



func (sw *RemoteClusterServiceWatcher) Initialize() error {

	// 1.We find the nginx ingress load balancer, in the case of docker for mac, the NodePort
	gatewaySelector, _ := 	labels.Parse("component=controller")

	gatewayServices, err := sw.apiClient.Svc().Lister().List(gatewaySelector)
	if err != nil {
		return fmt.Errorf("cannot obtain gateway services: %s", err)
	}


	gatewayService := gatewayServices[0]

	if gatewayService.Spec.Type != corev1.ServiceTypeNodePort {
		return fmt.Errorf("identity gateway service needs to be of type %s ", corev1.ServiceTypeNodePort)
	}

	// hacky as hell due to kind not supporting loadbalancers...
	endpoints, err := net.LookupHost("docker.for.mac.host.internal")
	if err != nil {
		return err
	}

	sw.gatewayService = &RemoteClusterGatewayService{
		gatewayService.Name,
		gatewayService.Namespace,
		endpoints[0],
		"30080",
	}

	sw.apiClient.Svc().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    sw.mirrorRemoteService,
			DeleteFunc: func(_ interface{}) {},
			UpdateFunc: func(_, obj interface{}) {},
		}, )

	return nil
}

type RemoteClusterConfigWatcher struct {
	k8sAPI *k8s.API
}

func NewRemoteClusterConfigWatcher(k8sAPI *k8s.API) *RemoteClusterConfigWatcher {
	rcw := &RemoteClusterConfigWatcher{
		k8sAPI: k8sAPI,
	}
	k8sAPI.Secret().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    rcw.registerRemoteCluster,
			DeleteFunc: func(_ interface{}) {},
			UpdateFunc: func(_, obj interface{}) {},
		},
	)
	return rcw
}

func (ew *RemoteClusterConfigWatcher) registerRemoteCluster(obj interface{}) {
	secret := obj.(*corev1.Secret)

	if secret.Type == mirrorSecretType {
		if val, ok := secret.Data[configKeyName]; ok {
			clientConfig, err := clientcmd.RESTConfigFromKubeConfig(val)
			if err != nil {
				log.Error(err)
			}
			if err != nil {
				log.Fatal("Error parsing kube config: %s", err)
			}

			watcher, err := NewRemoteClusterServiceWatcher(ew.k8sAPI, clientConfig, "remote")
			if err != nil {
				log.Fatal(err)
			}

			err = watcher.Initialize()
			if err != nil {
				log.Fatal("Could not initialize %s ", err)
			}
			log.Infof("Registered remote cluster: %s", clientConfig.Host)

		}
	}
}

func Main(args []string) {
	cmd := flag.NewFlagSet("service-mirror", flag.ExitOnError)

	kubeConfigPath := cmd.String("kubeconfig", "", "path to kube config")

	flags.ConfigureAndParse(cmd, args)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	k8sAPI, err := k8s.InitializeAPI(
		*kubeConfigPath,
		k8s.SC,
	)

	if err != nil {
		log.Fatalf("Failed to initialize K8s API: %s", err)
	}

	_ = NewRemoteClusterConfigWatcher(k8sAPI)
	k8sAPI.Sync()
	time.Sleep(100 * time.Hour) // wait forever... for now :)
}
