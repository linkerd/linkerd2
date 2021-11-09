package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/ghodss/yaml"

	"github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	"github.com/linkerd/linkerd2/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// This is a throwaway script for testing the destination service

func main() {

	config, err := k8s.GetConfig("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error configuring Kubernetes API client: %v", err)
		os.Exit(1)
	}

	client := versioned.NewForConfigOrDie(config)

	list, err := client.ServerV1beta1().Servers("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err.Error())
	}

	for _, server := range list.Items {
		fmt.Printf("======= %s =======\n", server.Name)
		b, _ := yaml.Marshal(server)
		fmt.Println(string(b))
		fmt.Println()
	}
}
