package multus

import (
	"context"
	"flag"
	"os"

	"github.com/linkerd/linkerd2/pkg/flags"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	multusapi "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	multusctrl "github.com/linkerd/linkerd2/controller/multus"
)

// Main executes the identity subcommand
func Main(args []string) {
	cmd := flag.NewFlagSet("multus", flag.ExitOnError)

	metricsAddr := cmd.String("metrics-address", ":8081", "Prometheus metrics bind address")
	probeAddr := cmd.String("probe-address", ":8082", "Health probe address")
	enableLeaderElection := cmd.Bool("leader-election", true, "Enable controller leader election")

	componentName := "linkerd-multus.linkerd.io"

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(cmd)

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	flags.ConfigureAndParse(cmd, args)

	setupLogger := ctrl.Log.WithName("setup")

	// Manager.
	var scheme = runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(multusapi.AddToScheme(scheme))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     *metricsAddr,
		HealthProbeBindAddress: *probeAddr,
		LeaderElection:         *enableLeaderElection,
		LeaderElectionID:       componentName,
	})
	if err != nil {
		setupLogger.Error(err, "Failed to setup Manager")
		os.Exit(1)
	}

	//
	// Create, initialize and run service
	//
	cniGen := multusctrl.NewCNIGenerator(context.Background(), mgr.GetClient(), componentName)

	if err := cniGen.SetupWithManager(mgr); err != nil {
		setupLogger.Error(err, "Can not setup controller")
		os.Exit(1)
	}

	if err := mgr.Start(context.Background()); err != nil {
		setupLogger.Error(err, "Can not start Manager")
		os.Exit(1)
	}

	setupLogger.Info("Normal shutdown")
}
