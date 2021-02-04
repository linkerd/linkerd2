package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang/protobuf/ptypes"
	pb "github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// newCmdRepair creates a new cobra command `repair` which re-creates the
// linkerd-config-overrides secret if it has been deleted.
func newCmdRepair() *cobra.Command {

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
			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			// Error out if linkerd-config-overrides already exists.
			_, err = k8sAPI.CoreV1().Secrets(controlPlaneNamespace).Get(cmd.Context(), "linkerd-config-overrides", metav1.GetOptions{})
			if err == nil {
				return errors.New("secret/linkerd-config-overrides already exists. If you need to regenerate this resource, please delete it before proceeding")
			}

			// Load the stored config
			config, err := k8sAPI.CoreV1().ConfigMaps(controlPlaneNamespace).Get(cmd.Context(), "linkerd-config", metav1.GetOptions{})
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

			// Reset version fields
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
			idCtx := pb.IdentityContext{
				TrustAnchorsPem:    values.Global.IdentityTrustAnchorsPEM,
				Scheme:             values.Identity.Issuer.Scheme,
				ClockSkewAllowance: ptypes.DurationProto(clockSkewDuration),
				IssuanceLifetime:   ptypes.DurationProto(issuanceLifetime),
				TrustDomain:        values.Global.IdentityTrustDomain,
			}

			// Populate identity values
			err = fetchIdentityValues(cmd.Context(), k8sAPI, &idCtx, &values)
			if err != nil {
				return fmt.Errorf("Failed to load issuer credentials: %s", err)
			}

			// Render
			overrides, err := renderOverrides(&values, controlPlaneNamespace, true)
			if err != nil {
				return fmt.Errorf("Failed to render overrides: %s", err)
			}

			fmt.Printf(string(overrides))

			return nil
		},
	}

	return cmd
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
	values.DebugContainer.Image.Version = defaults.DebugContainer.Image.Version
	values.Global.Proxy.Image.Version = defaults.Global.Proxy.Image.Version
	values.Global.CliVersion = defaults.Global.CliVersion
	values.Global.ControllerImageVersion = defaults.Global.ControllerImageVersion
	values.Global.LinkerdVersion = defaults.Global.LinkerdVersion
	return nil
}
