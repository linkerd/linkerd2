package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/issuercerts"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

var (
	// repairNotApplicableVersionRegex matches older versions of Linkerd i.e
	// 2.8 and below
	repairNotApplicableVersionRegex = regexp.MustCompile(`^stable-2\.[0-8]\.([0-9]+)$`)

	// repairApplicableVersionRegex matches versions 2.9 versions up to 2.9.4
	repairApplicableVersionRegex = regexp.MustCompile(`^stable-2\.9\.[0-4]$`)
)

// newCmdRepair creates a new cobra command `repair` which re-creates the
// linkerd-config-overrides secret if it has been deleted.
func newCmdRepair() *cobra.Command {

	var force bool
	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Output the secret/linkerd-config-overrides resource if it has been deleted",
		Long: `Output the secret/linkerd-config-overrides resource if it has been deleted.

The secret/linkerd-config-overrides resource is necessary to perform upgrades of
the Linkerd control plane using the linkerd upgrade command. If this resource
has been deleted, the linkerd repair command can make a best effort to restore
it.  It is recommended that you review the secret/linkerd-config-overrides
resource after running linkerd repair to avoid any unexpected behavior during
linkerd upgrade.`,
		Example: "  linkerd repair | kubectl apply -f -",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := repair(cmd.Context(), force)
			if err != nil {
				fmt.Fprintf(os.Stderr, err.Error()+"\n")
				os.Exit(1)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", force, "Force render even if the CLI and control-plane versions don't match")

	return cmd
}

func repair(ctx context.Context, forced bool) error {
	k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return err
	}

	// Error out if linkerd-config-overrides already exists.
	_, err = k8sAPI.CoreV1().Secrets(controlPlaneNamespace).Get(ctx, "linkerd-config-overrides", metav1.GetOptions{})
	if err == nil {
		return errors.New("secret/linkerd-config-overrides already exists. If you need to regenerate this resource, please delete it before proceeding")
	}

	// Check if the CLI version matches with that of the server
	clientVersion := version.Version
	var serverVersion string
	serverVersion, err = healthcheck.GetServerVersion(ctx, controlPlaneNamespace, k8sAPI)
	if err != nil {
		return err
	}

	if !forced && serverVersion != clientVersion {
		// Suggest directly upgrading to 2.9.4 or above for older versions
		if repairNotApplicableVersionRegex.Match([]byte(serverVersion)) {
			return fmt.Errorf("repair command is only applicable to 2.9 control-plane versions. Please try upgrading to the latest supported versions of Linkerd i.e 2.9.4 and above")
		}

		// Suggest 2.9.4 CLI version for all 2.9 server versions up to 2.9.4
		if repairApplicableVersionRegex.Match([]byte(serverVersion)) {
			return fmt.Errorf("Please run the repair command with a `stable-2.9.4` CLI.\nRun `curl -sL https://run.linkerd.io/install | LINKERD2_VERSION=\"stable-2.9.4\" sh` to install the server version of the CLI")
		}

		// Suggest server version for everything else. This includes all edge versions
		return fmt.Errorf("Please run the repair command with a CLI that has the same version as the control plane.\nRun `curl -sL https://run.linkerd.io/install | LINKERD2_VERSION=\"%s\" sh` to install the server version of the CLI", serverVersion)
	}

	// Load the stored config
	config, err := k8sAPI.CoreV1().ConfigMaps(controlPlaneNamespace).Get(ctx, k8s.ConfigConfigMapName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Failed to load linkerd-config: %s", err)
	}

	values := linkerd2.Values{}

	valuesRaw, ok := config.Data["values"]
	if !ok {
		return errors.New("values not found in linkerd-config")
	}

	err = yaml.Unmarshal([]byte(valuesRaw), &values)
	if err != nil {
		return fmt.Errorf("Failed to load values from linkerd-config: %s", err)
	}

	err = resetVersion(&values)
	if err != nil {
		return fmt.Errorf("Failed to reset version fields in linkerd-config: %s", err)
	}

	clockSkewDuration, err := time.ParseDuration(values.Identity.Issuer.ClockSkewAllowance)
	if err != nil {
		return fmt.Errorf("Failed to parse ClockSkewAllowance from linkerd-config: %s", err)
	}
	issuanceLifetime, err := time.ParseDuration(values.Identity.Issuer.IssuanceLifetime)
	if err != nil {
		return fmt.Errorf("Failed to parse IssuanceLifetime from linkerd-config: %s", err)
	}
	idCtx := identityContext{
		trustAnchorsPem:    values.IdentityTrustAnchorsPEM,
		scheme:             values.Identity.Issuer.Scheme,
		clockSkewAllowance: clockSkewDuration,
		issuanceLifetime:   issuanceLifetime,
		trustDomain:        values.IdentityTrustDomain,
	}

	// Populate identity values
	err = fetchIdentityValues(ctx, k8sAPI, idCtx, &values)
	if err != nil {
		return fmt.Errorf("Failed to load issuer credentials: %s", err)
	}

	valuesMap, err := values.ToMap()
	if err != nil {
		return fmt.Errorf("Failed to convert Values into a map: %s", err)
	}

	overrides, err := renderOverrides(valuesMap, true)
	if err != nil {
		return fmt.Errorf("Failed to render overrides: %s", err)
	}

	fmt.Print(string(overrides))

	return nil
}

// resetVersion sets all linkerd version fields to the chart's default version
// to ensure that these fields will be absent from the overrides secret.
// This is important because `linkerd repair` will likely be run from a
// different version of the CLI than the currently installed version of Linkerd
// and treating this difference as an override would prevent the upgrade from
// updating the version fields.
func resetVersion(values *linkerd2.Values) error {
	defaults, err := linkerd2.NewValues()
	if err != nil {
		return err
	}

	if values.DebugContainer != nil && values.DebugContainer.Image != nil {
		values.DebugContainer.Image.Version = defaults.DebugContainer.Image.Version
	}

	if values.Proxy != nil && values.Proxy.Image != nil {
		values.Proxy.Image.Version = defaults.Proxy.Image.Version
	}

	if values.ProxyInit != nil && values.ProxyInit.Image != nil {
		values.ProxyInit.Image.Version = defaults.ProxyInit.Image.Version
	}

	values.CliVersion = defaults.CliVersion
	values.ControllerImageVersion = defaults.ControllerImageVersion
	values.LinkerdVersion = defaults.LinkerdVersion
	return nil
}

type identityContext struct {
	trustAnchorsPem    string
	scheme             string
	clockSkewAllowance time.Duration
	issuanceLifetime   time.Duration
	trustDomain        string
}

// fetchIdentityValue checks the kubernetes API to fetch an existing
// linkerd identity configuration.
//
// This bypasses the public API so that we can access secrets and validate
// permissions.
func fetchIdentityValues(ctx context.Context, k kubernetes.Interface, idctx identityContext, values *charts.Values) error {
	if idctx.scheme == "" {
		// if this is empty, then we are upgrading from a version
		// that did not support issuer schemes. Just default to the
		// linkerd one.
		idctx.scheme = k8s.IdentityIssuerSchemeLinkerd
	}

	var trustAnchorsPEM string
	var issuerData *issuercerts.IssuerCertData
	var err error

	trustAnchorsPEM = idctx.trustAnchorsPem

	issuerData, err = fetchIssuer(ctx, k, trustAnchorsPEM, idctx.scheme)
	if err != nil {
		return err
	}

	values.IdentityTrustAnchorsPEM = trustAnchorsPEM
	values.Identity.Issuer.Scheme = idctx.scheme
	values.Identity.Issuer.ClockSkewAllowance = idctx.clockSkewAllowance.String()
	values.Identity.Issuer.IssuanceLifetime = idctx.issuanceLifetime.String()
	values.Identity.Issuer.TLS.KeyPEM = issuerData.IssuerKey
	values.Identity.Issuer.TLS.CrtPEM = issuerData.IssuerCrt

	return nil
}

func fetchIssuer(ctx context.Context, k kubernetes.Interface, trustPEM string, scheme string) (*issuercerts.IssuerCertData, error) {
	var (
		issuerData *issuercerts.IssuerCertData
		err        error
	)
	switch scheme {
	case string(corev1.SecretTypeTLS):
		// Do not return external issuer certs as no need of storing them in config and upgrade secrets
		// Also contradicts condition in https://github.com/linkerd/linkerd2/blob/main/cli/cmd/options.go#L550
		return &issuercerts.IssuerCertData{}, nil
	default:
		issuerData, err = issuercerts.FetchIssuerData(ctx, k, trustPEM, controlPlaneNamespace)
		if issuerData != nil && issuerData.TrustAnchors != trustPEM {
			issuerData.TrustAnchors = trustPEM
		}
	}
	if err != nil {
		return nil, err
	}

	return issuerData, nil
}
