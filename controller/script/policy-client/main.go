package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"sigs.k8s.io/yaml"

	"github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	"github.com/linkerd/linkerd2/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func main() {
	namespace := flag.String("namespace", "", "namespace of resource to get")
	flag.Parse()

	config, err := k8s.GetConfig("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error configuring Kubernetes API client: %v", err)
		os.Exit(1)
	}
	client := versioned.NewForConfigOrDie(config)

	srvWatch, err := client.ServerV1beta2().Servers(*namespace).Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to watch Servers: %s", err)
		os.Exit(1)
	}
	sazWatch, err := client.ServerauthorizationV1beta1().ServerAuthorizations(*namespace).Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to watch ServerAuthorizations: %s", err)
		os.Exit(1)
	}
	srvUpdates := srvWatch.ResultChan()
	sazUpdates := sazWatch.ResultChan()

	for {
		var update interface{}
		select {
		case u := <-srvUpdates:
			update = u
		case u := <-sazUpdates:
			update = u
		}
		b, _ := yaml.Marshal(update)
		fmt.Println(string(b))
	}
}
