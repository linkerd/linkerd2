package server

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	clientv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"time"
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
	k.informerFactory.Start(stop)
	k.informerFactory.WaitForCacheSync(stop)
}

func (k *KubernetesClient) getPod(podName, podNamespace string) (pod *v1.Pod, err error) {
	return k.podLister.Pods(podNamespace).Get(podName)
}

func (k *KubernetesClient) getSecret(secretName, secretNamespace string) (data map[string][]byte, err error) {
	secret, err := k.client.CoreV1().Secrets(string(secretNamespace)).Get(secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return secret.Data, nil
}

func (k *KubernetesClient) getConfigMap(configMapName, configMapNamespace string) (data map[string]string, err error) {
	configMap, err := k.client.CoreV1().ConfigMaps(string(configMapNamespace)).Get(configMapName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return configMap.Data, nil
}


