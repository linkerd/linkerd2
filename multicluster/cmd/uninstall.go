package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/linkerd/linkerd2/multicluster/static"
	"github.com/linkerd/linkerd2/pkg/charts"
	"github.com/linkerd/linkerd2/pkg/k8s"
	mc "github.com/linkerd/linkerd2/pkg/multicluster"
	"github.com/spf13/cobra"
	chartloader "helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"
)

func newMulticlusterUninstallCommand() *cobra.Command {
	options, err := newMulticlusterInstallOptionsWithDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s", err)
		os.Exit(1)
	}

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Output Kubernetes configs to uninstall the Linkerd multicluster add-on",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {

			rules := clientcmd.NewDefaultClientConfigLoadingRules()
			rules.ExplicitPath = kubeconfigPath
			loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
			config, err := loader.RawConfig()
			if err != nil {
				return err
			}

			if kubeContext != "" {
				config.CurrentContext = kubeContext
			}

			k, err := k8s.NewAPI(kubeconfigPath, config.CurrentContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			links, err := mc.GetLinks(cmd.Context(), k.DynamicClient)
			if err != nil {
				return err
			}

			if len(links) > 0 {
				err := []string{"Please unlink the following clusters before uninstalling multicluster:"}
				for _, link := range links {
					err = append(err, fmt.Sprintf("  * %s", link.TargetClusterName))
				}
				return errors.New(strings.Join(err, "\n"))
			}

			values, err := buildMulticlusterInstallValues(cmd.Context(), options)

			if err != nil {
				return err
			}

			// Render raw values and create chart config
			rawValues, err := yaml.Marshal(values)
			if err != nil {
				return err
			}

			files := []*chartloader.BufferedFile{
				{Name: chartutil.ChartfileName},
				{Name: "templates/namespace.yaml"},
				{Name: "templates/gateway.yaml"},
				{Name: "templates/remote-access-service-mirror-rbac.yaml"},
				{Name: "templates/link-crd.yaml"},
			}

			chart := &charts.Chart{
				Name:      helmMulticlusterDefaultChartName,
				Dir:       helmMulticlusterDefaultChartName,
				Namespace: controlPlaneNamespace,
				RawValues: rawValues,
				Files:     files,
				Fs:        static.Templates,
			}
			buf, err := chart.RenderNoPartials()
			if err != nil {
				return err
			}
			stdout.Write(buf.Bytes())
			stdout.Write([]byte("---\n"))

			return nil
		},
	}

	cmd.Flags().StringVar(&options.namespace, "namespace", options.namespace, "The namespace in which the multicluster add-on is to be installed. Must not be the control plane namespace. ")

	return cmd
}
