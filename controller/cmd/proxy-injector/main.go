package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/controller/proxy-injector"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/wait"
)

func main() {
	metricsAddr := flag.String("metrics-addr", ":9995", "address to serve scrapable metrics on")
	port := flag.String("port", "443", "port that this webhook admission server listens on")
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig")
	controllerNamespace := flag.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	certFile := flag.String("tls-cert-file", "/var/linkerd-io/identity/certificate.crt", "location of the webhook server's TLS cert file")
	keyFile := flag.String("tls-key-file", "/var/linkerd-io/identity/private-key.p8", "location of the webhook server's TLS private key file")
	trustAnchorsPath := flag.String("trust-anchors-path", "/var/linkerd-io/trust-anchors/trust-anchors.pem", "path to the CA trust anchors PEM file used to create the mutating webhook configuration")
	volumeMountsWaitTime := flag.Duration("volume-mounts-wait", 3*time.Minute, "maximum wait time for the secret volumes to mount before the timeout expires")
	webhookServiceName := flag.String("webhook-service", "proxy-injector.linkerd.io", "name of the admission webhook")
	flags.ConfigureAndParse()

	stop := make(chan os.Signal, 1)
	defer close(stop)
	signal.Notify(stop, os.Interrupt, os.Kill)

	k8sClient, err := k8s.NewClientSet(*kubeconfig)
	if err != nil {
		log.Fatal("failed to initialize Kubernetes client: ", err)
	}

	log.Infof("waiting for the trust anchors volume to mount (default wait time: %s)", volumeMountsWaitTime)
	if err := waitForMounts(*volumeMountsWaitTime, *trustAnchorsPath); err != context.Canceled {
		log.Fatalf("failed to mount the ca bundle: %s", err)
	}

	webhookConfig := injector.NewWebhookConfig(k8sClient, *controllerNamespace, *webhookServiceName, *trustAnchorsPath)

	mwc, err := webhookConfig.CreateOrUpdate()
	if err != nil {
		log.Fatalf("failed to create the mutating webhook configurations resource: ", err)
	}
	log.Info("created or updated mutating webhook configuration: ", mwc.ObjectMeta.SelfLink)

	log.Infof("waiting for the tls secrets to mount (wait at most %s)", volumeMountsWaitTime)
	if err := waitForMounts(*volumeMountsWaitTime, *certFile, *keyFile); err != context.Canceled {
		log.Fatalf("failed to mount the tls secrets: %s", err)
	}

	s, err := injector.NewWebhookServer(*port, *certFile, *keyFile, *controllerNamespace, k8sClient)
	if err != nil {
		log.Fatal("failed to initialize the webhook server: ", err)
	}

	ready := make(chan struct{})
	go s.SyncAPI(ready)

	go func() {
		log.Infof("listening at port %s (cert: %s, key: %s)", *port, *certFile, *keyFile)
		if err := s.ListenAndServeTLS("", ""); err != nil {
			if err == http.ErrServerClosed {
				return
			}
			log.Fatal(err)
		}
	}()
	go admin.StartServer(*metricsAddr, ready)

	<-stop
	log.Info("shutting down webhook server")
	if err := s.Shutdown(); err != nil {
		log.Error(err)
	}
}

func waitForMounts(timeout time.Duration, paths ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	wait.Until(func() {
		ready := 0
		for _, file := range paths {
			if _, err := os.Stat(file); err != nil {
				log.Infof("mount not ready: %s", file)
				return
			}

			ready += 1
			log.Infof("mount ready: %s", file)
			if ready == len(paths) {
				break
			}
		}

		cancel()
	}, time.Millisecond*500, ctx.Done())

	return ctx.Err()
}
