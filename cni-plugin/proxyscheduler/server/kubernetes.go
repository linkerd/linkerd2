package server

import (
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	clientv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"time"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

)

const (
	pollInterval = 100 * time.Millisecond
	pollTimeout  = 60 * time.Second
)

type KubernetesClient struct {
	client          *kubernetes.Clientset
	informerFactory informers.SharedInformerFactory
	podLister       clientv1.PodLister
}

func NewKubernetesClient() (*KubernetesClient, error) {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	kube, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	informerFactory := informers.NewSharedInformerFactory(kube, 5*time.Minute)
	podInformer := informerFactory.Core().V1().Pods()
	podLister := podInformer.Lister()

	return &KubernetesClient{
		client:          kube,
		informerFactory: informerFactory,
		podLister:       podLister,
	}, nil
}

func (k *KubernetesClient) Start(stop <-chan struct{}) {
	logrus.Info("Starting shared informers")
	k.informerFactory.Start(stop)

	logrus.Info("Waiting for caches to sync")
	k.informerFactory.WaitForCacheSync(stop)
}

func (k *KubernetesClient) getPod(podName, podNamespace string) (pod *v1.Pod, err error) {
	return k.podLister.Pods(podNamespace).Get(podName)
}

func (k *KubernetesClient) getSecret(secretName, secretNamespace string) (data map[string][]byte, err error) {
	// TODO: get secret from cache instead?
	secret, err := k.client.CoreV1().Secrets(string(secretNamespace)).Get(secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return secret.Data, nil
}


