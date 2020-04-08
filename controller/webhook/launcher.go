package webhook

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	v1machinary "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/credswatcher"
	"github.com/linkerd/linkerd2/pkg/flags"
	pkgk8s "github.com/linkerd/linkerd2/pkg/k8s"
	pkgtls "github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
)

const (
	eventTypeSkipped = "WebhookTLSUpdateSkipped"
	eventTypeUpdated = "WebhookTLSUpdated"
)

// Launch sets up and starts the webhook and metrics servers
func Launch(APIResources []k8s.APIResource, metricsPort uint32, handler handlerFunc, component, subcommand string, args []string) {
	cmd := flag.NewFlagSet(subcommand, flag.ExitOnError)

	metricsAddr := cmd.String("metrics-addr", fmt.Sprintf(":%d", metricsPort), "address to serve scrapable metrics on")
	addr := cmd.String("addr", ":8443", "address to serve on")
	kubeconfig := cmd.String("kubeconfig", "", "path to kubeconfig")

	flags.ConfigureAndParse(cmd, args)

	stop := make(chan os.Signal, 1)
	defer close(stop)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	k8sAPI, err := k8s.InitializeAPI(*kubeconfig, true, APIResources...)
	if err != nil {
		log.Fatalf("failed to initialize Kubernetes API: %s", err)
	}

	cfg, err := config.Global(pkgk8s.MountPathGlobalConfig)
	if err != nil {
		log.Fatalf("Failed to load config: %s", err.Error())
	}

	controllerNS := cfg.GetLinkerdNamespace()
	deployment, err := getDeployment(controllerNS, component)
	if err != nil {
		log.Fatalf("Failed to construct k8s event recorder: %s", err)
	}

	cred, err := pkgtls.ReadPEMCreds(pkgk8s.MountPathTLSKeyPEM, pkgk8s.MountPathTLSCrtPEM)
	if err != nil {
		log.Fatalf("failed to read TLS secrets: %s", err)
	}

	s, err := NewServer(k8sAPI, *addr, cred, handler, component)
	if err != nil {
		log.Fatalf("failed to initialize the webhook server: %s", err)
	}

	k8sAPI.Sync(nil)

	onTLSChange := func() (message, reason string, err error) {
		cred, err := pkgtls.ReadPEMCreds(pkgk8s.MountPathTLSKeyPEM, pkgk8s.MountPathTLSCrtPEM)
		if err != nil {
			message = fmt.Sprint("Skipping webhook tls update as certs could not be read from disk")
			reason = eventTypeSkipped
			return message, reason, err
		}

		cert, err := tls.X509KeyPair([]byte(cred.EncodePEM()), []byte(cred.EncodePrivateKeyPEM()))
		if err != nil {
			message = fmt.Sprint("Skipping webhook tls update as keypair could not be created")
			reason = eventTypeSkipped
			return message, reason, err
		}

		s.updateTLSCred(cert)
		message = fmt.Sprint("Updated webhook tls")
		reason = eventTypeUpdated

		return message, reason, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go s.Start()
	go admin.StartServer(*metricsAddr)
	go credswatcher.WatchCredChanges(ctx, pkgk8s.MountPathBaseTLS, onTLSChange, s.getEventRecordFunc(deployment))

	<-stop
	log.Info("shutting down webhook server")
	if err := s.Shutdown(ctx); err != nil {
		log.Error(err)
	}
}

func getDeployment(namespace, component string) (runtime.Object, error) {
	api, err := pkgk8s.NewAPI("", "", "", []string{}, 0)
	if err != nil {
		return nil, err
	}

	deployment, err := api.AppsV1().Deployments(namespace).Get(component, v1machinary.GetOptions{})
	if err != nil {
		return nil, err
	}

	return deployment, nil
}
