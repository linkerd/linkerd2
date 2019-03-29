package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	pb "github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type (
	upgradeOptions struct{ *installOptions }
)

func newUpgradeOptionsWithDefaults() *upgradeOptions {
	return &upgradeOptions{newInstallOptionsWithDefaults()}
}

func newCmdUpgrade() *cobra.Command {
	options := newUpgradeOptionsWithDefaults()
	flags := options.flagSet(pflag.ExitOnError)

	cmd := &cobra.Command{
		Use:   "upgrade [flags]",
		Short: "Output Kubernetes configs to upgrade an existing Linkerd control plane",
		Long:  "Output Kubernetes configs to upgrade an existing Linkerd control plane.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// We need a Kubernetes client to fetch configs and issuer secrets.
			k, err := options.newK8s()
			if err != nil {
				return fmt.Errorf("failed to create a kubernetes client: %s", err)
			}

			// We fetch the configs directly from kubernetes because we need to be able
			// to upgrade/reinstall the control plane when the API is not available; and
			// this also serves as a passive check that we have privileges to access this
			// control plane.
			configs, err := fetchConfigs(k)
			if err != nil {
				return fmt.Errorf("could not fetch configs from kubernetes: %s", err)
			}

			// We recorded flags during a prior install. If we haven't overridden the
			// flag on this upgrade, reset that prior value as if it were specified now.
			setOptionsFromInstall(flags, configs.GetInstall())

			if err = options.validate(); err != nil {
				return err
			}

			// Save off the updated set of flags into the installOptions so it gets
			// persisted with the upgraded config.
			options.recordFlags(flags)

			// Update the configs from the synthesized options.
			options.overrideConfigs(configs, map[string]string{})
			configs.GetInstall().Flags = options.recordedFlags

			values, err := options.buildValuesWithoutIdentity(configs)
			if err != nil {
				return fmt.Errorf("could not build install configuration: %s", err)
			}

			identityValues, err := fetchIdentityValues(k, options.controllerReplicas, configs.GetGlobal().GetIdentityContext())
			if err != nil {
				fmt.Fprintln(os.Stderr, "Unable to fetch the existing issuer credentials from Kubernetes.")
				fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				os.Exit(1)
			}
			values.Identity = identityValues

			if err = values.render(os.Stdout, configs); err != nil {
				return fmt.Errorf("could not render install configuration: %s", err)
			}

			return nil
		},
	}

	cmd.PersistentFlags().AddFlagSet(flags)
	return cmd
}

func setOptionsFromInstall(flags *pflag.FlagSet, install *pb.Install) {
	for _, i := range install.GetFlags() {
		if f := flags.Lookup(i.GetName()); f != nil && !f.Changed {
			f.Value.Set(i.GetValue())
			f.Changed = true
		}
	}
}

func (options *upgradeOptions) newK8s() (*kubernetes.Clientset, error) {
	if options.ignoreCluster {
		return nil, errors.New("--ignore-cluster cannot be used with upgrade")
	}

	c, err := k8s.GetConfig(kubeconfigPath, kubeContext)
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(c)
}

// fetchConfigs checks the kubernetes API to fetch an existing
// linkerd configuration.
//
// This bypasses the public API so that upgrades can proceed when the API pod is
// not available.
func fetchConfigs(k *kubernetes.Clientset) (*pb.All, error) {
	configMap, err := k.CoreV1().
		ConfigMaps(controlPlaneNamespace).
		Get(k8s.ConfigConfigMapName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return config.FromConfigMap(configMap.Data)
}

// fetchIdentityValue checks the kubernetes API to fetch an existing
// linkerd identity configuration.
//
// This bypasses the public API so that we can access secrets and validate
// permissions.
func fetchIdentityValues(k *kubernetes.Clientset, replicas uint, idctx *pb.IdentityContext) (*installIdentityValues, error) {
	if idctx == nil {
		return nil, nil
	}

	keyPEM, crtPEM, expiry, err := fetchIssuer(k, idctx.GetTrustAnchorsPem())
	if err != nil {
		return nil, err
	}

	return &installIdentityValues{
		Replicas:        replicas,
		TrustDomain:     idctx.GetTrustDomain(),
		TrustAnchorsPEM: idctx.GetTrustAnchorsPem(),
		Issuer: &issuerValues{
			ClockSkewAllowance:  idctx.GetClockSkewAllowance().String(),
			IssuanceLifetime:    idctx.GetIssuanceLifetime().String(),
			CrtExpiryAnnotation: k8s.IdentityIssuerExpiryAnnotation,

			KeyPEM:    keyPEM,
			CrtPEM:    crtPEM,
			CrtExpiry: expiry,
		},
	}, nil
}

func fetchIssuer(k *kubernetes.Clientset, trustPEM string) (string, string, time.Time, error) {
	roots, err := tls.DecodePEMCertPool(trustPEM)
	if err != nil {
		return "", "", time.Time{}, err
	}

	secret, err := k.CoreV1().
		Secrets(controlPlaneNamespace).
		Get(k8s.IdentityIssuerSecretName, metav1.GetOptions{})
	if err != nil {
		return "", "", time.Time{}, err
	}

	keyPEM := string(secret.Data[k8s.IdentityIssuerKeyName])
	key, err := tls.DecodePEMKey(keyPEM)
	if err != nil {
		return "", "", time.Time{}, err
	}

	crtPEM := string(secret.Data[k8s.IdentityIssuerCrtName])
	crt, err := tls.DecodePEMCrt(crtPEM)
	if err != nil {
		return "", "", time.Time{}, err
	}

	cred := &tls.Cred{PrivateKey: key, Crt: *crt}
	if err = cred.Verify(roots, ""); err != nil {
		return "", "", time.Time{}, fmt.Errorf("invalid issuer credentials: %s", err)
	}

	return keyPEM, crtPEM, crt.Certificate.NotAfter, nil
}
