package cmd

import (
	"context"
	"regexp"

	"github.com/fatih/color"
	"github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	defaultLinkerdNamespace              = "linkerd"
	defaultMulticlusterNamespace         = "linkerd-multicluster"
	defaultGatewayName                   = "linkerd-gateway"
	helmMulticlusterLinkDefaultChartName = "linkerd-multicluster-link"
	tokenKey                             = "token"

	saNameAnnotationKey       = "kubernetes.io/service-account.name"
	defaultServiceAccountName = "linkerd-service-mirror-remote-access-default"

	clusterNameLabel        = "multicluster.linkerd.io/cluster-name"
	trustDomainAnnotation   = "multicluster.linkerd.io/trust-domain"
	clusterDomainAnnotation = "multicluster.linkerd.io/cluster-domain"
)

var (
	HelmMulticlusterDefaultChartName = "linkerd-multicluster"

	apiAddr               string // An empty value means "use the Kubernetes configuration"
	controlPlaneNamespace string
	kubeconfigPath        string
	kubeContext           string
	impersonate           string
	impersonateGroup      []string
	verbose               bool

	// special handling for Windows, on all other platforms these resolve to
	// os.Stdout and os.Stderr, thanks to https://github.com/mattn/go-colorable
	stdout = color.Output
	stderr = color.Error

	// These regexs are not as strict as they could be, but are a quick and dirty
	// sanity check against illegal characters.
	alphaNumDashDot = regexp.MustCompile(`^[\.a-zA-Z0-9-]+$`)
)

// NewCmdMulticluster returns a new multicluster command
func NewCmdMulticluster() *cobra.Command {

	multiclusterCmd := &cobra.Command{
		Use:     "multicluster [flags]",
		Aliases: []string{"mc"},
		Args:    cobra.NoArgs,
		Short:   "Manages the multicluster setup for Linkerd",
		Long: `Manages the multicluster setup for Linkerd.

This command provides subcommands to manage the multicluster support
functionality of Linkerd. You can use it to install the service mirror
components on a cluster, manage credentials and link clusters together.`,
		Example: `  # Install multicluster addons.
  linkerd --context=cluster-a multicluster install | kubectl --context=cluster-a apply -f -

  # Extract mirroring cluster credentials from cluster A and install them on cluster B
  linkerd --context=cluster-a multicluster link --cluster-name=target | kubectl apply --context=cluster-b -f -`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if verbose {
				log.SetLevel(log.DebugLevel)
			} else {
				log.SetLevel(log.PanicLevel)
			}
			return nil
		},
	}

	multiclusterCmd.PersistentFlags().StringVarP(&controlPlaneNamespace, "linkerd-namespace", "L", defaultLinkerdNamespace, "Namespace in which Linkerd is installed")
	multiclusterCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests")
	multiclusterCmd.PersistentFlags().StringVar(&kubeContext, "context", "", "Name of the kubeconfig context to use")
	multiclusterCmd.PersistentFlags().StringVar(&impersonate, "as", "", "Username to impersonate for Kubernetes operations")
	multiclusterCmd.PersistentFlags().StringArrayVar(&impersonateGroup, "as-group", []string{}, "Group to impersonate for Kubernetes operations")
	multiclusterCmd.PersistentFlags().StringVar(&apiAddr, "api-addr", "", "Override kubeconfig and communicate directly with the control plane at host:port (mostly for testing)")
	multiclusterCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Turn on debug logging")
	multiclusterCmd.AddCommand(newLinkCommand())
	multiclusterCmd.AddCommand(newUnlinkCommand())
	multiclusterCmd.AddCommand(newGenCommand())
	multiclusterCmd.AddCommand(newMulticlusterInstallCommand())
	multiclusterCmd.AddCommand(NewCmdCheck())
	multiclusterCmd.AddCommand(newMulticlusterUninstallCommand())
	multiclusterCmd.AddCommand(newGatewaysCommand())
	multiclusterCmd.AddCommand(newAllowCommand())

	// resource-aware completion flag configurations
	pkgcmd.ConfigureNamespaceFlagCompletion(
		multiclusterCmd, []string{"linkerd-namespace"},
		kubeconfigPath, impersonate, impersonateGroup, kubeContext)

	pkgcmd.ConfigureKubeContextFlagCompletion(multiclusterCmd, kubeconfigPath)
	return multiclusterCmd
}

func getLinkerdConfigMap(ctx context.Context) (*linkerd2.Values, error) {
	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return nil, err
	}

	_, values, err := healthcheck.FetchCurrentConfiguration(ctx, kubeAPI, controlPlaneNamespace)
	if err != nil {
		return nil, err
	}

	return values, nil
}
