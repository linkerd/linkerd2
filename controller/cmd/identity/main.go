package identity

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/golang/protobuf/ptypes"
	idctl "github.com/linkerd/linkerd2/controller/identity"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/identity"
	"github.com/linkerd/linkerd2/pkg/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
)

// TODO watch trustAnchorsPath for changes
// TODO watch issuerPath for changes
// TODO restrict servicetoken audiences (and lifetimes)

// Main executes the identity subcommand
func Main(args []string) {
	cmd := flag.NewFlagSet("identity", flag.ExitOnError)

	addr := cmd.String("addr", ":8080", "address to serve on")
	adminAddr := cmd.String("admin-addr", ":9990", "address of HTTP admin server")
	kubeConfigPath := cmd.String("kubeconfig", "", "path to kube config")
	issuerPath := cmd.String("issuer",
		"/var/run/linkerd/identity/issuer",
		"path to directory containing issuer credentials")
	externalMode := cmd.Bool("external-issuer", false, "whether we use external cert manager")

	flags.ConfigureAndParse(cmd, args)

	cfg, err := config.Global(consts.MountPathGlobalConfig)
	if err != nil {
		log.Fatalf("Failed to load config: %s", err.Error())
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	controllerNS := cfg.GetLinkerdNamespace()
	idctx := cfg.GetIdentityContext()
	if idctx == nil {
		log.Infof("Identity disabled in control plane configuration.")
		os.Exit(0)
	}

	trustDomain := idctx.GetTrustDomain()
	dom, err := idctl.NewTrustDomain(controllerNS, trustDomain)
	if err != nil {
		log.Fatalf("Invalid trust domain: %s", err.Error())
	}

	trustAnchors, err := tls.DecodePEMCertPool(idctx.GetTrustAnchorsPem())
	if err != nil {
		log.Fatalf("Failed to read trust anchors: %s", err)
	}

	keyName := consts.IdentityIssuerKeyName
	crtName := consts.IdentityIssuerCrtName

	if *externalMode {
		keyName = consts.IdentityIssuerKeyNameExternal
		crtName = consts.IdentityIssuerCrtNameExternal
	}

	validity := tls.Validity{
		ClockSkewAllowance: tls.DefaultClockSkewAllowance,
		Lifetime:           identity.DefaultIssuanceLifetime,
	}
	if pbd := idctx.GetClockSkewAllowance(); pbd != nil {
		csa, err := ptypes.Duration(pbd)
		if err != nil {
			log.Warnf("Invalid clock skew allowance: %s", err)
		} else {
			validity.ClockSkewAllowance = csa
		}
	}
	if pbd := idctx.GetIssuanceLifetime(); pbd != nil {
		il, err := ptypes.Duration(pbd)
		if err != nil {
			log.Warnf("Invalid issuance lifetime: %s", err)
		} else {
			validity.Lifetime = il
		}
	}

	expectedName := fmt.Sprintf("identity.%s.%s", controllerNS, trustDomain)
	watcher := idctl.NewFsCredsWatcher(*issuerPath, keyName, crtName, expectedName, trustAnchors, validity)

	if err := watcher.StartWatching(); err != nil {
		log.Fatalf("Failed to start creds watcher: %s", err)
	}

	k8s, err := k8s.NewAPI(*kubeConfigPath, "", "", 0)
	if err != nil {
		log.Fatalf("Failed to load kubeconfig: %s: %s", *kubeConfigPath, err)
	}
	v, err := idctl.NewK8sTokenValidator(k8s, dom)
	if err != nil {
		log.Fatalf("Failed to initialize identity service: %s", err)
	}

	svc := identity.NewService(v, watcher.Creds())

	go admin.StartServer(*adminAddr)
	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %s", *addr, err)
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
