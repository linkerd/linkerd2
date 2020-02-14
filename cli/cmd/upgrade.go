package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/issuercerts"

	pb "github.com/linkerd/linkerd2/controller/gen/config"
	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	controlPlaneMessage    = "Don't forget to run `linkerd upgrade control-plane`!"
	failMessage            = "For troubleshooting help, visit: https://linkerd.io/upgrade/#troubleshooting\n"
	trustRootChangeMessage = "Rotating the trust anchors will affect existing proxies\nSee https://linkerd.io/2/tasks/rotating_identity_certificates/ for more information"
)

type upgradeOptions struct {
	manifests string
	force     bool
	*installOptions

	verifyTLS func(tls *charts.TLS, service string) error
}

func newUpgradeOptionsWithDefaults() (*upgradeOptions, error) {
	installOptions, err := newInstallOptionsWithDefaults()
	if err != nil {
		return nil, err
	}

	return &upgradeOptions{
		manifests:      "",
		installOptions: installOptions,
		verifyTLS:      verifyWebhookTLS,
	}, nil
}

// upgradeOnlyFlagSet includes flags that are only accessible at upgrade-time
// and not at install-time. also these flags are not intended to be persisted
// via linkerd-config ConfigMap, unlike recordableFlagSet
func (options *upgradeOptions) upgradeOnlyFlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("upgrade-only", pflag.ExitOnError)

	flags.StringVar(
		&options.manifests, "from-manifests", options.manifests,
		"Read config from a Linkerd install YAML rather than from Kubernetes",
	)
	flags.BoolVar(
		&options.force, "force", options.force,
		"Force upgrade operation even when issuer certificate does not work with the roots of all proxies",
	)

	return flags
}

// newCmdUpgradeConfig is a subcommand for `linkerd upgrade config`
func newCmdUpgradeConfig(options *upgradeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config [flags]",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes cluster-wide resources to upgrade an existing Linkerd",
		Long: `Output Kubernetes cluster-wide resources to upgrade an existing Linkerd.

Note that this command should be followed by "linkerd upgrade control-plane".`,
		Example: `  # Default upgrade.
  linkerd upgrade config | kubectl apply --prune -l linkerd.io/control-plane-ns=linkerd -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return upgradeRunE(options, configStage, options.recordableFlagSet())
		},
	}

	cmd.Flags().AddFlagSet(options.allStageFlagSet())

	return cmd
}

// newCmdUpgradeControlPlane is a subcommand for `linkerd upgrade control-plane`
func newCmdUpgradeControlPlane(options *upgradeOptions) *cobra.Command {
	flags := options.recordableFlagSet()

	cmd := &cobra.Command{
		Use:   "control-plane [flags]",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes control plane resources to upgrade an existing Linkerd",
		Long: `Output Kubernetes control plane resources to upgrade an existing Linkerd.

Note that the default flag values for this command come from the Linkerd control
plane. The default values displayed in the Flags section below only apply to the
install command. It should be run after "linkerd upgrade config".`,
		Example: `  # Default upgrade.
  linkerd upgrade control-plane | kubectl apply --prune -l linkerd.io/control-plane-ns=linkerd -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return upgradeRunE(options, controlPlaneStage, flags)
		},
	}

	cmd.PersistentFlags().AddFlagSet(flags)

	return cmd
}

func newCmdUpgrade() *cobra.Command {
	options, err := newUpgradeOptionsWithDefaults()
	if err != nil {
		upgradeErrorf(err.Error())
	}

	flags := options.recordableFlagSet()
	upgradeOnlyFlags := options.upgradeOnlyFlagSet()

	cmd := &cobra.Command{
		Use:   "upgrade [flags]",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes configs to upgrade an existing Linkerd control plane",
		Long: `Output Kubernetes configs to upgrade an existing Linkerd control plane.

Note that the default flag values for this command come from the Linkerd control
plane. The default values displayed in the Flags section below only apply to the
install command.`,

		Example: `  # Default upgrade.
  linkerd upgrade | kubectl apply --prune -l linkerd.io/control-plane-ns=linkerd -f -

  # Similar to install, upgrade may also be broken up into two stages, by user
  # privilege.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return upgradeRunE(options, "", flags)
		},
	}

	cmd.Flags().AddFlagSet(flags)
	cmd.PersistentFlags().AddFlagSet(upgradeOnlyFlags)

	cmd.AddCommand(newCmdUpgradeConfig(options))
	cmd.AddCommand(newCmdUpgradeControlPlane(options))

	return cmd
}

func upgradeRunE(options *upgradeOptions, stage string, flags *pflag.FlagSet) error {
	if options.ignoreCluster {
		panic("ignore cluster must be unset") // Programmer error.
	}

	// We need a Kubernetes client to fetch configs and issuer secrets.
	var k kubernetes.Interface
	var err error
	if options.manifests != "" {
		readers, err := read(options.manifests)
		if err != nil {
			upgradeErrorf("Failed to parse manifests from %s: %s", options.manifests, err)
		}

		k, err = k8s.NewFakeAPIFromManifests(readers)
		if err != nil {
			upgradeErrorf("Failed to parse Kubernetes objects from manifest %s: %s", options.manifests, err)
		}
	} else {
		k, err = k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
		if err != nil {
			upgradeErrorf("Failed to create a kubernetes client: %s", err)
		}
	}

	values, _, err := options.validateAndBuild(stage, k, flags)
	if err != nil {
		upgradeErrorf("Failed to build upgrade configuration: %s", err)
	}

	// rendering to a buffer and printing full contents of buffer after
	// render is complete, to ensure that okStatus prints separately
	var buf bytes.Buffer
	if err = render(&buf, values); err != nil {
		upgradeErrorf("Could not render upgrade configuration: %s", err)
	}

	buf.WriteTo(os.Stdout)

	if options.identityOptions.trustPEMFile != "" {
		fmt.Fprintf(os.Stderr, "\n%s %s\n", warnStatus, trustRootChangeMessage)
	}

	if stage == configStage {
		fmt.Fprintf(os.Stderr, "%s\n", controlPlaneMessage)
	}

	return nil
}

func (options *upgradeOptions) validateAndBuild(stage string, k kubernetes.Interface, flags *pflag.FlagSet) (*charts.Values, *pb.All, error) {
	if err := options.validate(); err != nil {
		return nil, nil, err
	}

	// We fetch the configs directly from kubernetes because we need to be able
	// to upgrade/reinstall the control plane when the API is not available; and
	// this also serves as a passive check that we have privileges to access this
	// control plane.
	_, configs, err := healthcheck.FetchLinkerdConfigMap(k, controlPlaneNamespace)
	if err != nil {
		return nil, nil, fmt.Errorf("could not fetch configs from kubernetes: %s", err)
	}

	// If the configs need to be repaired--either because sections did not
	// exist or because it is missing expected fields, repair it.
	repairConfigs(configs)

	// We recorded flags during a prior install. If we haven't overridden the
	// flag on this upgrade, reset that prior value as if it were specified now.
	//
	// This implies that the default flag values for the upgrade command come
	// from the control-plane, and not from the defaults specified in the FlagSet.
	setFlagsFromInstall(flags, configs.GetInstall().GetFlags())

	// Save off the updated set of flags into the installOptions so it gets
	// persisted with the upgraded config.
	options.recordFlags(flags)

	// Update the configs from the synthesized options.
	// The overrideConfigs() is used to override proxy configs only.
	options.overrideConfigs(configs, map[string]string{})

	// Override configs with upgrade CLI options.
	if options.controlPlaneVersion != "" {
		configs.GetGlobal().Version = options.controlPlaneVersion
	}
	configs.GetInstall().Flags = options.recordedFlags
	configs.GetGlobal().OmitWebhookSideEffects = options.omitWebhookSideEffects
	if configs.GetGlobal().GetClusterDomain() == "" {
		configs.GetGlobal().ClusterDomain = defaultClusterDomain
	}

	if options.identityOptions.crtPEMFile != "" || options.identityOptions.keyPEMFile != "" {

		if configs.Global.IdentityContext.Scheme == string(corev1.SecretTypeTLS) {
			return nil, nil, errors.New("cannot update issuer certificates if you are using external cert management solution")
		}

		if options.identityOptions.crtPEMFile == "" {
			return nil, nil, errors.New("a certificate file must be specified if a private key is provided")
		}
		if options.identityOptions.keyPEMFile == "" {
			return nil, nil, errors.New("a private key file must be specified if a certificate is provided")
		}
		if err := checkFilesExist([]string{options.identityOptions.crtPEMFile, options.identityOptions.keyPEMFile}); err != nil {
			return nil, nil, err
		}
	}

	if options.identityOptions.trustPEMFile != "" {
		if err := checkFilesExist([]string{options.identityOptions.trustPEMFile}); err != nil {
			return nil, nil, err
		}
	}

	var identity *identityWithAnchorsAndTrustDomain
	idctx := configs.GetGlobal().GetIdentityContext()
	if idctx.GetTrustDomain() == "" || idctx.GetTrustAnchorsPem() == "" {
		// If there wasn't an idctx, or if it doesn't specify the required fields, we
		// must be upgrading from a version that didn't support identity, so generate it anew...
		identity, err = options.identityOptions.genValues()
		if err != nil {
			return nil, nil, err
		}
		configs.GetGlobal().IdentityContext = toIdentityContext(identity)
	} else {
		identity, err = options.fetchIdentityValues(k, idctx)
		if err != nil {
			return nil, nil, err
		}
	}

	// Values have to be generated after any missing identity is generated,
	// otherwise it will be missing from the generated configmap.
	values, err := options.buildValuesWithoutIdentity(configs)
	if err != nil {
		return nil, nil, fmt.Errorf("could not build install configuration: %s", err)
	}
	values.Identity = identity.Identity
	values.Global.IdentityTrustAnchorsPEM = identity.TrustAnchorsPEM
	values.Global.IdentityTrustDomain = identity.TrustDomain
	// we need to do that if we have updated the anchors as the config map json has already been generated
	if values.Global.IdentityTrustAnchorsPEM != configs.Global.IdentityContext.TrustAnchorsPem {
		// override the anchors in config
		configs.Global.IdentityContext.TrustAnchorsPem = values.Global.IdentityTrustAnchorsPEM
		// rebuild the json config map
		globalJSON, _, _, _ := config.ToJSON(configs)
		values.Configs.Global = globalJSON
	}

	// if exist, re-use the proxy injector, profile validator and tap TLS secrets.
	// otherwise, let Helm generate them by creating an empty charts.TLS struct here.
	proxyInjectorTLS, err := fetchTLSSecret(k, k8s.ProxyInjectorWebhookServiceName, options)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return nil, nil, fmt.Errorf("could not fetch existing proxy injector secret: %s", err)
		}
		proxyInjectorTLS = &charts.TLS{}
	}
	values.ProxyInjector = &charts.ProxyInjector{TLS: proxyInjectorTLS}

	profileValidatorTLS, err := fetchTLSSecret(k, k8s.SPValidatorWebhookServiceName, options)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return nil, nil, fmt.Errorf("could not fetch existing profile validator secret: %s", err)
		}
		profileValidatorTLS = &charts.TLS{}
	}
	values.ProfileValidator = &charts.ProfileValidator{TLS: profileValidatorTLS}

	tapTLS, err := fetchTLSSecret(k, k8s.TapServiceName, options)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return nil, nil, fmt.Errorf("could not fetch existing tap secret: %s", err)
		}
		tapTLS = &charts.TLS{}
	}
	values.Tap = &charts.Tap{TLS: tapTLS}

	values.Stage = stage

	return values, configs, nil
}

func setFlagsFromInstall(flags *pflag.FlagSet, installFlags []*pb.Install_Flag) {
	for _, i := range installFlags {
		if f := flags.Lookup(i.GetName()); f != nil && !f.Changed {
			f.Value.Set(i.GetValue())
			f.Changed = true
		}
	}
}

func repairConfigs(configs *pb.All) {
	// Repair the "install" section; install flags are updated separately
	if configs.Install == nil {
		configs.Install = &pb.Install{}
	}
	// ALWAYS update the CLI version to the most recent.
	configs.Install.CliVersion = version.Version

	// Repair the "proxy" section
	if configs.Proxy == nil {
		configs.Proxy = &pb.Proxy{}
	}
	if configs.Proxy.DebugImage == nil {
		configs.Proxy.DebugImage = &pb.Image{}
	}
}

func fetchTLSSecret(k kubernetes.Interface, webhook string, options *upgradeOptions) (*charts.TLS, error) {
	secret, err := k.CoreV1().
		Secrets(controlPlaneNamespace).
		Get(webhookSecretName(webhook), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	value := &charts.TLS{
		KeyPEM: string(secret.Data["key.pem"]),
		CrtPEM: string(secret.Data["crt.pem"]),
	}

	if err := options.verifyTLS(value, webhook); err != nil {
		return nil, err
	}

	return value, nil
}

func ensureIssuerCertWorksWithAllProxies(k kubernetes.Interface, cred *tls.Cred) error {
	meshedPods, err := healthcheck.GetMeshedPodsIdentityData(k, "")
	var problematicPods []string
	if err != nil {
		return err
	}
	for _, pod := range meshedPods {
		roots, err := tls.DecodePEMCertPool(pod.Anchors)

		if roots != nil {
			err = cred.Verify(roots, "", time.Time{})
		}

		if err != nil {
			problematicPods = append(problematicPods, fmt.Sprintf("* %s/%s", pod.Namespace, pod.Name))
		}
	}

	if len(problematicPods) > 0 {
		errorMessageHeader := "You are attempting to use an issuer certificate which does not validate against the trust roots of the following pods:"
		errorMessageFooter := "These pods do not have the current trust bundle and must be restarted.  Use the --force flag to proceed anyway (this will likely prevent those pods from sending or receiving traffic)."
		return fmt.Errorf("%s\n\t%s\n%s", errorMessageHeader, strings.Join(problematicPods, "\n\t"), errorMessageFooter)
	}
	return nil
}

// fetchIdentityValue checks the kubernetes API to fetch an existing
// linkerd identity configuration.
//
// This bypasses the public API so that we can access secrets and validate
// permissions.
func (options *upgradeOptions) fetchIdentityValues(k kubernetes.Interface, idctx *pb.IdentityContext) (*identityWithAnchorsAndTrustDomain, error) {
	if idctx == nil {
		return nil, nil
	}

	if idctx.Scheme == "" {
		// if this is empty, then we are upgrading from a version
		// that did not support issuer schemes. Just default to the
		// linkerd one.
		idctx.Scheme = k8s.IdentityIssuerSchemeLinkerd
	}

	var trustAnchorsPEM string
	var issuerData *issuercerts.IssuerCertData
	var err error

	if options.identityOptions.trustPEMFile != "" {
		trustb, err := ioutil.ReadFile(options.identityOptions.trustPEMFile)
		if err != nil {
			return nil, err
		}
		trustAnchorsPEM = string(trustb)
	} else {
		trustAnchorsPEM = idctx.GetTrustAnchorsPem()
	}

	updatingIssuerCert := options.identityOptions.crtPEMFile != "" && options.identityOptions.keyPEMFile != ""

	if updatingIssuerCert {
		issuerData, err = readIssuer(trustAnchorsPEM, options.identityOptions.crtPEMFile, options.identityOptions.keyPEMFile)
	} else {
		issuerData, err = fetchIssuer(k, trustAnchorsPEM, idctx.Scheme)
	}
	if err != nil {
		return nil, err
	}

	cred, err := issuerData.VerifyAndBuildCreds("")
	if err != nil {
		return nil, fmt.Errorf("issuer certificate does not work with the provided roots: %s\nFor more information: https://linkerd.io/2/tasks/rotating_identity_certificates/", err)
	}
	issuerData.Expiry = &cred.Crt.Certificate.NotAfter

	if updatingIssuerCert && !options.force {
		if err := ensureIssuerCertWorksWithAllProxies(k, cred); err != nil {
			return nil, err
		}
	}

	return &identityWithAnchorsAndTrustDomain{
		TrustDomain:     idctx.GetTrustDomain(),
		TrustAnchorsPEM: trustAnchorsPEM,
		Identity: &charts.Identity{

			Issuer: &charts.Issuer{
				Scheme:              idctx.Scheme,
				ClockSkewAllowance:  idctx.GetClockSkewAllowance().String(),
				IssuanceLifetime:    idctx.GetIssuanceLifetime().String(),
				CrtExpiry:           *issuerData.Expiry,
				CrtExpiryAnnotation: k8s.IdentityIssuerExpiryAnnotation,
				TLS: &charts.TLS{
					KeyPEM: issuerData.IssuerKey,
					CrtPEM: issuerData.IssuerCrt,
				},
			},
		},
	}, nil

}

func readIssuer(trustPEM, issuerCrtPath, issuerKeyPath string) (*issuercerts.IssuerCertData, error) {
	key, crt, err := issuercerts.LoadIssuerCrtAndKeyFromFiles(issuerKeyPath, issuerCrtPath)
	if err != nil {
		return nil, err
	}
	issuerData := &issuercerts.IssuerCertData{
		TrustAnchors: trustPEM,
		IssuerCrt:    crt,
		IssuerKey:    key,
	}

	return issuerData, nil
}

func fetchIssuer(k kubernetes.Interface, trustPEM string, scheme string) (*issuercerts.IssuerCertData, error) {
	var (
		issuerData *issuercerts.IssuerCertData
		err        error
	)
	switch scheme {
	case string(corev1.SecretTypeTLS):
		issuerData, err = issuercerts.FetchExternalIssuerData(k, controlPlaneNamespace)
	default:
		issuerData, err = issuercerts.FetchIssuerData(k, trustPEM, controlPlaneNamespace)
		if issuerData != nil && issuerData.TrustAnchors != trustPEM {
			issuerData.TrustAnchors = trustPEM
		}
	}
	if err != nil {
		return nil, err
	}

	return issuerData, nil
}

// upgradeErrorf prints the error message and quits the upgrade process
func upgradeErrorf(format string, a ...interface{}) {
	template := fmt.Sprintf("%s %s\n%s\n", failStatus, format, failMessage)
	fmt.Fprintf(os.Stderr, template, a...)
	os.Exit(1)
}

func webhookCommonName(webhook string) string {
	return fmt.Sprintf("%s.%s.svc", webhook, controlPlaneNamespace)
}

func webhookSecretName(webhook string) string {
	return fmt.Sprintf("%s-tls", webhook)
}

func verifyWebhookTLS(value *charts.TLS, webhook string) error {
	crt, err := tls.DecodePEMCrt(value.CrtPEM)
	if err != nil {
		return err
	}
	roots := crt.CertPool()
	if err := crt.Verify(roots, webhookCommonName(webhook), time.Time{}); err != nil {
		return err
	}

	return nil
}
