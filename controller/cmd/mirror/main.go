package mirror

import (
	"fmt"
	"io/ioutil"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Main executes the heartbeat subcommand
func Main(args []string) {

	tokenFile := "/var/run/linkerd/mirror/credentials/token"
	caCrtFile := "/var/run/linkerd/mirror/credentials/ca.crt"
	hostFile := "/var/run/linkerd/mirror/credentials/api"

	token, err := ioutil.ReadFile(tokenFile)
	if err != nil {
		fmt.Println(err.Error())
	}

	host, err := ioutil.ReadFile(hostFile)
	if err != nil {
		fmt.Println(err.Error())
	}

	config := rest.Config{
		Host:            string(host),
		BearerToken:     string(token),
		BearerTokenFile: tokenFile,
		TLSClientConfig: rest.TLSClientConfig{
			CAFile: caCrtFile,
		},
	}

	k, err := kubernetes.NewForConfig(&config)
	if err != nil {
		fmt.Println(err.Error())
	}

	services, err := k.CoreV1().Services("").List(metav1.ListOptions{})
	if err != nil {
		fmt.Println(err)
	}

	for _, s := range services.Items {
		if len(s.Spec.ExternalIPs) > 0 {
			fmt.Printf("%s (%s)", s.Name, s.Spec.ExternalIPs)
		} else {
			fmt.Println(s.Name)
		}
	}
}
