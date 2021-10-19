package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/ghodss/yaml"

	"github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	"github.com/linkerd/linkerd2/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

func main() {
	resource := flag.String("resource", "server", "kind of policy resource to get")
	namespace := flag.String("namespace", "", "namespace of resource to get")
	flag.Parse()

	config, err := k8s.GetConfig("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error configuring Kubernetes API client: %v", err)
		os.Exit(1)
	}
	client := versioned.NewForConfigOrDie(config)

	var watch watch.Interface
	switch *resource {
	case "server", "srv":
		watch, err = client.ServerV1beta1().Servers(*namespace).Watch(context.Background(), metav1.ListOptions{})
	case "serverauthorization", "saz":
		watch, err = client.ServerauthorizationV1beta1().ServerAuthorizations(*namespace).Watch(context.Background(), metav1.ListOptions{})
	default:
		log.Fatalf("Unknown policy resource: %s; supported resources: server, srv, serverauthorization, saz", *resource)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to watch resource %s: %s", *resource, err)
		os.Exit(1)
	}

	updates := watch.ResultChan()
	for {
		update := <-updates
		b, _ := yaml.Marshal(update)
		fmt.Println(string(b))
	}
}
