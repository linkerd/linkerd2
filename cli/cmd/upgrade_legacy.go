package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/golang/protobuf/ptypes"
	"github.com/linkerd/linkerd2/cli/flag"
	pb "github.com/linkerd/linkerd2/controller/gen/config"
	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/issuercerts"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

func loadStoredValuesLegacy(ctx context.Context, k *k8s.KubernetesAPI) (*charts.Values, error) {

	// We fetch the configs directly from kubernetes because we need to be able
	// to upgrade/reinstall the control plane when the API is not available; and
	// this also serves as a passive check that we have privileges to access this
	// control plane.
	_, configs, err := healthcheck.FetchLinkerdConfigMap(ctx, k, controlPlaneNamespace)
	if err != nil {
		return nil, fmt.Errorf("could not fetch configs from kubernetes: %s", err)
	}
	repairConfigs(configs)

	values, err := charts.NewValues(false)
	if err != nil {
		return nil, err
	}
	allStageFlags, allStageFlagSet := makeAllStageFlags(values)
	installFlags, installFlagSet := makeInstallFlags(values)
	upgradeFlags, installUpgradeFlagSet, err := makeInstallUpgradeFlags(values)
	if err != nil {
		return nil, err
	}
	proxyFlags, proxyFlagSet := makeProxyFlags(values)

	flagSet := pflag.NewFlagSet("loaded_flags", pflag.ExitOnError)
	flagSet.AddFlagSet(allStageFlagSet)
	flagSet.AddFlagSet(installFlagSet)
	flagSet.AddFlagSet(installUpgradeFlagSet)
	flagSet.AddFlagSet(proxyFlagSet)

	setFlagsFromInstall(flagSet, configs.GetInstall().GetFlags())

	flags := flattenFlags(allStageFlags, installFlags, upgradeFlags, proxyFlags)
	err = flag.ApplySetFlags(values, flags)
	if err != nil {
		return nil, err
	}

	idctx := configs.GetGlobal().GetIdentityContext()
	if idctx.GetTrustDomain() != "" && idctx.GetTrustAnchorsPem() != "" {
		err = fetchIdentityValues(ctx, k, idctx, values)
		if err != nil {
			return nil, err
		}
	}

	if !addOnOverwrite {
		// Update Add-Ons Configuration from the linkerd-value cm
		cmRawValues, _ := k8s.GetAddOnsConfigMap(ctx, k, controlPlaneNamespace)
		if cmRawValues != nil {
			//Cm is present now get the data
			cmData, ok := cmRawValues["values"]
			if !ok {
				return nil, fmt.Errorf("values subpath not found in %s configmap", k8s.AddOnsConfigMapName)
			}

			// repair Add-On configs
			repairedCm, err := repairAddOnConfig([]byte(cmData))
			if err == nil {
				// Update only if there is no error
				cmData = string(repairedCm)
			} else {
				log.Warnf("add-on config repair failed: %s", err)
			}

			if err = yaml.Unmarshal([]byte(cmData), &values); err != nil {
				return nil, err
			}
		}
	}

	return values, nil
}

func repairAddOnConfig(rawValues []byte) ([]byte, error) {

	var values map[string]interface{}
	err := yaml.Unmarshal(rawValues, &values)
	if err != nil {
		return nil, err
	}

	// Grafana Depreciation Fix
	// Convert into Map instead of Values, as the latter returns with empty values
	if grafana, err := healthcheck.GetMap(values, "grafana"); err == nil {
		image, err := healthcheck.GetMap(grafana, "image")
		if err == nil {
			// Remove image.name tag if only name is present and set to the older image tag
			if val, err := healthcheck.GetString(image, "name"); err == nil && val == "gcr.io/linkerd-io/grafana" {
				delete(image, "name")
			}

			// Remove image tag if its a empty map
			if len(image) == 0 {
				delete(grafana, "image")
			}
		}

		// Handle removal of grafana.name field
		name, err := healthcheck.GetString(grafana, "name")
		if err == nil {
			// If default, remove it as its no longer needed
			if name == "linkerd-grafana" {
				delete(grafana, "name")
			}
		}

	}
	rawValues, err = yaml.Marshal(values)
	if err != nil {
		return nil, err
	}
	return rawValues, nil
}

func setFlagsFromInstall(flags *pflag.FlagSet, installFlags []*pb.Install_Flag) {
	for _, i := range installFlags {
		if f := flags.Lookup(i.GetName()); f != nil && !f.Changed {
			// The function recordFlags() stores the string representation of flags in the ConfigMap
			// so a stringSlice is stored e.g. as [a,b].
			// To avoid having f.Value.Set() interpreting that as a string we need to remove
			// the brackets
			value := i.GetValue()
			if f.Value.Type() == "stringSlice" {
				value = strings.Trim(value, "[]")
			}

			f.Value.Set(value)
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
	if configs.GetProxy().GetDebugImage().GetImageName() == "" {
		configs.Proxy.DebugImage.ImageName = k8s.DebugSidecarImage
	}
	if configs.GetProxy().GetDebugImageVersion() == "" {
		configs.Proxy.DebugImageVersion = version.Version
	}
}

// fetchIdentityValue checks the kubernetes API to fetch an existing
// linkerd identity configuration.
//
// This bypasses the public API so that we can access secrets and validate
// permissions.
func fetchIdentityValues(ctx context.Context, k kubernetes.Interface, idctx *pb.IdentityContext, values *charts.Values) error {
	if idctx == nil {
		return nil
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

	trustAnchorsPEM = idctx.GetTrustAnchorsPem()

	issuerData, err = fetchIssuer(ctx, k, trustAnchorsPEM, idctx.Scheme)
	if err != nil {
		return err
	}

	clockSkewDuration, err := ptypes.Duration(idctx.GetClockSkewAllowance())
	if err != nil {
		return fmt.Errorf("could not convert clock skew protobuf Duration format into golang Duration: %s", err)
	}

	issuanceLifetimeDuration, err := ptypes.Duration(idctx.GetIssuanceLifetime())
	if err != nil {
		return fmt.Errorf("could not convert issuance Lifetime protobuf Duration format into golang Duration: %s", err)
	}

	values.Global.IdentityTrustAnchorsPEM = trustAnchorsPEM
	values.Identity.Issuer.Scheme = idctx.Scheme
	values.Identity.Issuer.ClockSkewAllowance = clockSkewDuration.String()
	values.Identity.Issuer.IssuanceLifetime = issuanceLifetimeDuration.String()
	if issuerData.Expiry != nil {
		values.Identity.Issuer.CrtExpiry = *issuerData.Expiry
	}
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
