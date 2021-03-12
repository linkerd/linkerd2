package identity

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	idctl "github.com/linkerd/linkerd2/controller/identity"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/identity"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/pkg/trace"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	v1machinery "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

const envTrustAnchors = "LINKERD2_IDENTITY_TRUST_ANCHORS"

// Main executes the identity subcommand
func Main(args []string) {
	cmd := flag.NewFlagSet("identity", flag.ExitOnError)

	addr := cmd.String("addr", ":8080", "address to serve on")
	adminAddr := cmd.String("admin-addr", ":9990", "address of HTTP admin server")
	kubeConfigPath := cmd.String("kubeconfig", "", "path to kube config")
	controllerNS := cmd.String("controller-namespace", "", "namespace in which Linkerd is installed")
	identityScheme := cmd.String("identity-scheme", "", "scheme used for the identity issuer secret format")
	trustDomain := cmd.String("identity-trust-domain", "", "configures the name suffix used for identities")
	identityIssuanceLifeTime := cmd.String("identity-issuance-lifetime", "", "the amount of time for which the Identity issuer should certify identity")
	identityClockSkewAllowance := cmd.String("identity-clock-skew-allowance", "", "the amount of time to allow for clock skew within a Linkerd cluster")

	issuerPath := cmd.String("issuer",
		"/var/run/linkerd/identity/issuer",
		"path to directory containing issuer credentials")

	var issuerPathCrt string
	var issuerPathKey string
	traceCollector := flags.AddTraceFlags(cmd)
	componentName := "linkerd-identity"

	flags.ConfigureAndParse(cmd, args)

	encodedIdentityTrustAnchorPEM := os.Getenv(envTrustAnchors)
	rawPEM, err := base64.StdEncoding.DecodeString(encodedIdentityTrustAnchorPEM)
	if err != nil {
		log.Fatalf("could not decode identity trust anchors PEM: %s", err.Error())
	}

	identityTrustAnchorPEM := string(rawPEM)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if *identityScheme == "" || *trustDomain == "" {
		log.Infof("Identity disabled in control plane configuration.")
		os.Exit(0)
	}

	if *identityScheme == k8s.IdentityIssuerSchemeLinkerd {
		issuerPathCrt = filepath.Join(*issuerPath, k8s.IdentityIssuerCrtName)
		issuerPathKey = filepath.Join(*issuerPath, k8s.IdentityIssuerKeyName)
	} else {
		issuerPathCrt = filepath.Join(*issuerPath, corev1.TLSCertKey)
		issuerPathKey = filepath.Join(*issuerPath, corev1.TLSPrivateKeyKey)
	}

	dom, err := idctl.NewTrustDomain(*controllerNS, *trustDomain)
	if err != nil {
		log.Fatalf("Invalid trust domain: %s", err.Error())
	}

	trustAnchors, err := tls.DecodePEMCertPool(identityTrustAnchorPEM)
	if err != nil {
		log.Fatalf("Failed to read trust anchors: %s", err)
	}

	validity := tls.Validity{
		ClockSkewAllowance: tls.DefaultClockSkewAllowance,
		Lifetime:           identity.DefaultIssuanceLifetime,
	}
	if pbd := *identityClockSkewAllowance; pbd != "" {
		csa, err := time.ParseDuration(pbd)
		if err != nil {
			log.Warnf("Invalid clock skew allowance: %s", err)
		} else {
			validity.ClockSkewAllowance = csa
		}
	}
	if pbd := *identityIssuanceLifeTime; pbd != "" {
		il, err := time.ParseDuration(pbd)
		if err != nil {
			log.Warnf("Invalid issuance lifetime: %s", err)
		} else {
			validity.Lifetime = il
		}
	}

	expectedName := fmt.Sprintf("identity.%s.%s", *controllerNS, *trustDomain)
	issuerEvent := make(chan struct{})
	issuerError := make(chan error)

	//
	// Create and start FS creds watcher
	//
	watcher := tls.NewFsCredsWatcher(*issuerPath, issuerEvent, issuerError)
	go func() {
		if err := watcher.StartWatching(ctx); err != nil {
			log.Fatalf("Failed to start creds watcher: %s", err)
		}
	}()

	//
	// Create k8s API
	//
	k8sAPI, err := k8s.NewAPI(*kubeConfigPath, "", "", []string{}, 0)
	if err != nil {
		log.Fatalf("Failed to load kubeconfig: %s: %s", *kubeConfigPath, err)
	}
	v, err := idctl.NewK8sTokenValidator(ctx, k8sAPI, dom)
	if err != nil {
		log.Fatalf("Failed to initialize identity service: %s", err)
	}

	// Create K8s event recorder
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: k8sAPI.CoreV1().Events(*controllerNS),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: componentName})
	deployment, err := k8sAPI.AppsV1().Deployments(*controllerNS).Get(ctx, componentName, v1machinery.GetOptions{})

	if err != nil {
		log.Fatalf("Failed to construct k8s event recorder: %s", err)
	}

	recordEventFunc := func(eventType, reason, message string) {
		recorder.Event(deployment, eventType, reason, message)
	}

	//
	// Create, initialize and run service
	//
	svc := identity.NewService(v, trustAnchors, &validity, recordEventFunc, expectedName, issuerPathCrt, issuerPathKey)
	if err = svc.Initialize(); err != nil {
		log.Fatalf("Failed to initialize identity service: %s", err)
	}
	go func() {
		svc.Run(issuerEvent, issuerError)
	}()

	//
	// Bind and serve
	//
	go admin.StartServer(*adminAddr)
	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %s", *addr, err)
	}

	if *traceCollector != "" {
		if err := trace.InitializeTracing(componentName, *traceCollector); err != nil {
			log.Warnf("failed to initialize tracing: %s", err)
		}
	}
	srv := prometheus.NewGrpcServer()
	identity.Register(srv, svc)
	go func() {
		log.Infof("starting gRPC server on %s", *addr)
		srv.Serve(lis)
	}()
	<-stop
	log.Infof("shutting down gRPC server on %s", *addr)
	srv.GracefulStop()
}
