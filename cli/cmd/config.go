package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

func newCmdConfig() *cobra.Command {

	outputFormat := "yaml"

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Print the Linkerd config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {

			k, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, 0)
			if err != nil {
				upgradeErrorf("Failed to create a kubernetes client: %s", err)
			}

			_, configPb, err := healthcheck.FetchLinkerdConfigMap(k, controlPlaneNamespace)
			if err != nil {
				return err
			}

			global, proxy, install, err := config.ToJSON(configPb)
			if err != nil {
				return err
			}

			configs, err := unmarshalConfigs(global, proxy, install)
			if err != nil {
				return err
			}

			if outputFormat == "yaml" {
				err = printYaml(configs)
			} else if outputFormat == "json" {
				err = printJSON(configs)
			} else {
				err = fmt.Errorf("Unknown output format: %s", outputFormat)
			}
			if err != nil {
				return err
			}

			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", outputFormat, "Output format; one of: \"json\" or \"yaml\"")

	return cmd
}

func unmarshalConfigs(global, proxy, install string) (map[string]interface{}, error) {
	globalConfig, err := unmarshal(global)
	if err != nil {
		return nil, err
	}
	proxyConfig, err := unmarshal(proxy)
	if err != nil {
		return nil, err
	}
	installConfig, err := unmarshal(install)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"global":  globalConfig,
		"proxy":   proxyConfig,
		"install": installConfig,
	}, nil
}

func unmarshal(in string) (map[string]interface{}, error) {
	var data map[string]interface{}

	err := json.Unmarshal([]byte(in), &data)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func printJSON(configs map[string]interface{}) error {
	bytes, err := json.MarshalIndent(configs, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(bytes))
	return nil
}

func printYaml(configs map[string]interface{}) error {
	bytes, err := yaml.Marshal(configs)
	if err != nil {
		return err
	}
	fmt.Println(string(bytes))
	return nil
}
