package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/linkerd/linkerd2/controller/proxy-injector"
	"github.com/linkerd/linkerd2/pkg/flags"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	port                 = ""
	kubeconfig           = ""
	controllerNamespace  = ""
	certFile             = ""
	keyFile              = ""
	trustAnchorsPath     = ""
	volumeMountsWaitTime = ""
	webhookServiceName   = ""
)

func init() {
	flag.StringVar(&port, "port", "443", "port that this webhook admission server listens on")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig")
	flag.StringVar(&controllerNamespace, "controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	flag.StringVar(&certFile, "tls-cert-file", "/var/linkerd-io/identity/certificate.crt", "location of the webhook server's TLS cert file")
	flag.StringVar(&keyFile, "tls-key-file", "/var/linkerd-io/identity/private-key.p8", "location of the webhook server's TLS private key file")
	flag.StringVar(&trustAnchorsPath, "trust-anchors-path", "/var/linkerd-io/trust-anchors/trust-anchors.pem", "path to the CA trust anchors PEM file used to create the mutating webhook configuration")
	flag.StringVar(&volumeMountsWaitTime, "volume-mounts-wait", "3m", "maximum wait time for the secret volumes to mount before the timeout expires")
	flag.StringVar(&webhookServiceName, "webhook-service", "proxy-injector.linkerd.io", "name of the admission webhook")
	flags.ConfigureAndParse()
}

func main() {
	stop := make(chan os.Signal, 1)
	defer close(stop)
	signal.Notify(stop, os.Interrupt, os.Kill)

	k8sClient, err := injector.NewClientset(kubeconfig)
	if err != nil {
		log.Fatal("failed to initialize Kubernetes client: ", err)
	}

	log.Infof("waiting for the trust anchors volume to mount (default wait time: %s)", volumeMountsWaitTime)
	if err := waitForMounts(trustAnchorsPath); err != context.Canceled {
		log.Fatalf("failed to mount the ca bundle: %s", err)
	}

	webhookConfig := injector.NewWebhookConfig(k8sClient, controllerNamespace, webhookServiceName, trustAnchorsPath)
	webhookConfig.SyncAPI(nil)

	exist, err := webhookConfig.Exist()
	if err != nil {
		log.Fatalf("failed to access the mutating webhook configurations resource: ", err)
	}

	if !exist {
		log.Info("creating mutating webhook configuration")
		webhookConfig, err := webhookConfig.Create()
		if err != nil {
			log.Fatal("faild to create the mutating webhook configuration: ", err)
		}
		log.Info("created mutating webhook configuration: ", webhookConfig.ObjectMeta.SelfLink)
	}

	log.Infof("waiting for the tls secrets to mount (wait at most %s)", volumeMountsWaitTime)
	if err := waitForMounts(certFile, keyFile); err != context.Canceled {
		log.Fatalf("failed to mount the tls secrets: %s", err)
	}

	s, err := injector.NewWebhookServer(port, certFile, keyFile, controllerNamespace, k8sClient)
	if err != nil {
		log.Fatal("failed to initialize the webhook server: ", err)
	}
	s.SyncAPI(nil)

	s.SetLogLevel(log.StandardLogger().Level)

	go func() {
		log.Infof("listening at port %s (cert: %s, key: %s)", port, certFile, keyFile)
		if err := s.ListenAndServeTLS("", ""); err != nil {
			if err == http.ErrServerClosed {
				return
			}
			log.Fatal(err)
		}
	}()

	<-stop
	log.Info("shutting down webhook server")
	if err := s.Shutdown(); err != nil {
		log.Error(err)
	}
}

func waitForMounts(paths ...string) error {
	timeout, err := time.ParseDuration(volumeMountsWaitTime)
	if err != nil {
		log.Fatalf("failed to parse volume timeout duration: %s", err)
	}

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
