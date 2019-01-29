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
	k8sPkg "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/wait"
)

func main() {
	metricsAddr := flag.String("metrics-addr", ":9995", "address to serve scrapable metrics on")
	addr := flag.String("addr", ":8443", "address to serve on")
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig")
	controllerNamespace := flag.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	volumeMountsWaitTime := flag.Duration("volume-mounts-wait", 3*time.Minute, "maximum wait time for the secret volumes to mount before the timeout expires")
	webhookServiceName := flag.String("webhook-service", "linkerd-proxy-injector.linkerd.io", "name of the admission webhook")
	flags.ConfigureAndParse()

	stop := make(chan os.Signal, 1)
	defer close(stop)
	signal.Notify(stop, os.Interrupt, os.Kill)

	k8sClient, err := k8s.NewClientSet(*kubeconfig)
	if err != nil {
		log.Fatalf("failed to initialize Kubernetes client: %s", err)
	}

	log.Infof("waiting for the trust anchors volume to mount at %s", k8sPkg.MountPathTLSTrustAnchor)
	if err := waitForMounts(*volumeMountsWaitTime, k8sPkg.MountPathTLSTrustAnchor); err != context.Canceled {
		log.Fatalf("failed to mount the ca bundle: %s", err)
	}

	webhookConfig, err := injector.NewWebhookConfig(k8sClient, *controllerNamespace, *webhookServiceName, k8sPkg.MountPathTLSTrustAnchor)
	if err != nil {
		log.Fatalf("failed to read the trust anchor file: %s", err)
	}

	mwc, err := webhookConfig.CreateOrUpdate()
	if err != nil {
		log.Fatalf("failed to create the mutating webhook configurations resource: %s", err)
	}
	log.Infof("created or updated mutating webhook configuration: %s", mwc.ObjectMeta.SelfLink)

	var (
		certFile = k8sPkg.MountPathTLSIdentityCert
		keyFile  = k8sPkg.MountPathTLSIdentityKey
	)
	log.Infof("waiting for the tls secrets to mount at %s and %s", certFile, keyFile)
	if err := waitForMounts(*volumeMountsWaitTime, certFile, keyFile); err != context.Canceled {
		log.Fatalf("failed to mount the tls secrets: %s", err)
	}

	resources := &injector.WebhookResources{
		FileProxySpec:                k8sPkg.MountPathConfigProxySpec,
		FileProxyInitSpec:            k8sPkg.MountPathConfigProxyInitSpec,
		FileTLSTrustAnchorVolumeSpec: k8sPkg.MountPathTLSTrustAnchorVolumeSpec,
		FileTLSIdentityVolumeSpec:    k8sPkg.MountPathTLSIdentityVolumeSpec,
	}
	s, err := injector.NewWebhookServer(k8sClient, resources, *addr, *controllerNamespace, certFile, keyFile)
	if err != nil {
		log.Fatalf("failed to initialize the webhook server: %s", err)
	}

	go func() {
		log.Infof("listening at %s", *addr)
		if err := s.ListenAndServeTLS("", ""); err != nil {
			if err == http.ErrServerClosed {
				return
			}
			log.Fatal(err)
		}
	}()
	go admin.StartServer(*metricsAddr)

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

			ready++
			log.Infof("mount ready: %s", file)
			if ready == len(paths) {
				break
			}
		}

		cancel()
	}, time.Millisecond*500, ctx.Done())

	return ctx.Err()
}
