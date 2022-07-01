package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta1"
	beta1 "github.com/linkerd/linkerd2/controller/gen/apis/serverauthorization/v1beta1"
	l5dcrdinformer "github.com/linkerd/linkerd2/controller/gen/client/informers/externalversions"
	pkgK8s "github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/k8s"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// K8sResources holds cached pods, services, servers and saz
type K8sResources struct {
	Pods                 *v1.PodList
	Services             *v1.ServiceList
	Servers              []*v1beta1.Server
	ServerAuthorizations []*beta1.ServerAuthorization
}

// FetchK8sResources fetch K8s resources to minimize network activity while working with them
func FetchK8sResources(ctx context.Context, namespace string) (*K8sResources, error) {
	k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return nil, err
	}

	lr5dAPI := initServerAPI(kubeconfigPath)

	pods, err := k8sAPI.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	services, err := k8sAPI.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	servers, err := lr5dAPI.Server().V1beta1().Servers().Lister().Servers(namespace).List(labels.NewSelector())
	if err != nil {
		return nil, err
	}

	serverAuthorizations, err := lr5dAPI.Serverauthorization().V1beta1().ServerAuthorizations().Lister().ServerAuthorizations(namespace).List(labels.NewSelector())
	if err != nil {
		return nil, err
	}

	return &K8sResources{
		Pods:                 pods,
		Services:             services,
		Servers:              servers,
		ServerAuthorizations: serverAuthorizations,
	}, nil
}

func initServerAPI(kubeconfigPath string) l5dcrdinformer.SharedInformerFactory {
	config, err := k8s.GetConfig(kubeconfigPath, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	lr5dClient, err := pkgK8s.NewL5DCRDClient(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	lr5dAPI := l5dcrdinformer.NewSharedInformerFactory(lr5dClient, 10*time.Minute)

	stopCh := make(chan struct{})
	go lr5dAPI.Server().V1beta1().Servers().Informer().Run(stopCh)
	go lr5dAPI.Serverauthorization().V1beta1().ServerAuthorizations().Informer().Run(stopCh)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if !cache.WaitForCacheSync(ctx.Done(), lr5dAPI.Server().V1beta1().Servers().Informer().HasSynced, lr5dAPI.Serverauthorization().V1beta1().ServerAuthorizations().Informer().HasSynced) {
		fmt.Fprintln(os.Stderr, "failed to initialized client")
		//nolint:gocritic
		os.Exit(1)
	}

	return lr5dAPI
}
