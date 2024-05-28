package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/linkerd/linkerd2/multicluster/static"
	mccharts "github.com/linkerd/linkerd2/multicluster/values"
	"github.com/linkerd/linkerd2/pkg/charts"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/spf13/cobra"
	chartloader "helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

type (
	allowOptions struct {
		namespace          string
		serviceAccountName string
		ignoreCluster      bool
		output             string
	}
)

func newAllowCommand() *cobra.Command {
	opts := allowOptions{
		namespace:     defaultMulticlusterNamespace,
		ignoreCluster: false,
		output:        "yaml",
	}

	cmd := &cobra.Command{
		Hidden: false,
		Use:    "allow",
		Short:  "Outputs credential resources that allow service-mirror controllers to connect to this cluster",
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {

			values, err := buildMulticlusterAllowValues(cmd.Context(), &opts)
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
				{Name: "templates/remote-access-service-mirror-rbac.yaml"},
			}

			chart := &charts.Chart{
				Name:      helmMulticlusterDefaultChartName,
				Dir:       helmMulticlusterDefaultChartName,
				Namespace: opts.namespace,
				RawValues: rawValues,
				Files:     files,
				Fs:        static.Templates,
			}
			buf, err := chart.Render()
			if err != nil {
				return err
			}

			if opts.output == "json" {
				reader := yamlDecoder.NewYAMLReader(bufio.NewReaderSize(&buf, 4096))
				for {
					manifest, err := reader.Read()
					if err != nil {
						if errors.Is(err, io.EOF) {
							break
						}
						return err
					}
					bytes, err := yaml.YAMLToJSON(manifest)
					if err != nil {
						return err
					}
					_, err = stdout.Write(append(bytes, '\n'))
					if err != nil {
						return err
					}
				}
				return nil
			} else if opts.output == "yaml" {
				stdout.Write(buf.Bytes())
				stdout.Write([]byte("---\n"))
				return nil
			}
			return fmt.Errorf("Invalid output format: %s", opts.output)
		},
	}

	cmd.Flags().StringVar(&opts.namespace, "namespace", defaultMulticlusterNamespace, "The destination namespace for the service account.")
	cmd.Flags().BoolVar(&opts.ignoreCluster, "ignore-cluster", false, "Ignore cluster configuration")
	cmd.Flags().StringVar(&opts.serviceAccountName, "service-account-name", "", "The name of the multicluster access service account")
	cmd.PersistentFlags().StringVarP(&opts.output, "output", "o", "yaml", "Output format. One of: json|yaml")

	pkgcmd.ConfigureNamespaceFlagCompletion(
		cmd, []string{"namespace"},
		kubeconfigPath, impersonate, impersonateGroup, kubeContext)

	return cmd
}

func buildMulticlusterAllowValues(ctx context.Context, opts *allowOptions) (*mccharts.Values, error) {

	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return nil, err
	}

	if opts.namespace == "" {
		return nil, errors.New("you need to specify a namespace")
	}

	if opts.serviceAccountName == "" {
		return nil, errors.New("you need to specify a service account name")
	}

	defaults, err := mccharts.NewInstallValues()
	if err != nil {
		return nil, err
	}

	defaults.LinkerdVersion = version.Version
	defaults.Gateway.Enabled = false
	defaults.ServiceMirror = false
	defaults.RemoteMirrorServiceAccount = true
	defaults.RemoteMirrorServiceAccountName = opts.serviceAccountName

	if !opts.ignoreCluster {
		acc, err := kubeAPI.CoreV1().ServiceAccounts(opts.namespace).Get(ctx, defaults.RemoteMirrorServiceAccountName, metav1.GetOptions{})
		if err == nil && acc != nil {
			return nil, fmt.Errorf("Service account with name %s already exists, use --ignore-cluster for force operation", defaults.RemoteMirrorServiceAccountName)
		}
		if !kerrors.IsNotFound(err) {
			return nil, err
		}
	}

	return defaults, nil
}
