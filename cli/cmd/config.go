package cmd

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"encoding/json"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/spf13/cobra"
)


func newCmdConfig() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Print the Linkerd config",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {

			k, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, 0)
			if err != nil {
				upgradeErrorf("Failed to create a kubernetes client: %s", err)
			}

			_, configs, err := healthcheck.FetchLinkerdConfigMap(k, controlPlaneNamespace)

			global, proxy, install, err := config.ToJSON(configs)

			fmt.Printf("Global:\n%s\n", prettify(global))
			fmt.Printf("Proxy:\n%s\n", prettify(proxy))
			fmt.Printf("Install:\n%s\n", prettify(install))
		},
	}

	return cmd
}

func prettify(in string) string {

	var data map[string]interface{}

	err := json.Unmarshal([]byte(in), &data)
	if err != nil {
		log.Fatalf("error parsing json: %s", err)
	}

	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Fatalf("error serializing json: %s", err)
	}
	return string(bytes)
}