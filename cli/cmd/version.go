package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const defaultVersionString = "unavailable"

type versionOptions struct {
	shortVersion      bool
	onlyClientVersion bool
	proxy             bool
	namespace         string
}

func newVersionOptions() *versionOptions {
	return &versionOptions{
		shortVersion:      false,
		onlyClientVersion: false,
		proxy:             false,
		namespace:         "",
	}
}

func newCmdVersion() *cobra.Command {
	options := newVersionOptions()

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the client and server version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var k8sAPI *k8s.KubernetesAPI
			var err error
			if !options.onlyClientVersion {
				k8sAPI, err = k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
				if err != nil {
					return err
				}
			}

			configureAndRunVersion(k8sAPI, options, os.Stdout)
			return nil
		},
	}

	cmd.PersistentFlags().BoolVar(&options.shortVersion, "short", options.shortVersion, "Print the version number(s) only, with no additional output")
	cmd.PersistentFlags().BoolVar(&options.onlyClientVersion, "client", options.onlyClientVersion, "Print the client version only")
	cmd.PersistentFlags().BoolVar(&options.proxy, "proxy", options.proxy, "Print data-plane versions")
	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace to use for --proxy versions (default: all namespaces)")

	return cmd
}

func configureAndRunVersion(
	k8sAPI *k8s.KubernetesAPI,
	options *versionOptions,
	stdout io.Writer,
) {
	clientVersion := version.Version
	if options.shortVersion {
		fmt.Fprintln(stdout, clientVersion)
	} else {
		fmt.Fprintf(stdout, "Client version: %s\n", clientVersion)
	}

	if !options.onlyClientVersion {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		serverVersion, err := healthcheck.GetServerVersion(ctx, controlPlaneNamespace, k8sAPI)
		if err != nil {
			serverVersion = defaultVersionString
		}

		if options.shortVersion {
			fmt.Fprintln(stdout, serverVersion)
		} else {
			fmt.Fprintf(stdout, "Server version: %s\n", serverVersion)
		}

		if options.proxy {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			selector := fmt.Sprintf("%s=%s", k8s.ControllerNSLabel, controlPlaneNamespace)
			podList, err := k8sAPI.CoreV1().Pods(options.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
			if err != nil {
				fmt.Fprintln(stdout, "Proxy versions: unavailable")
			} else {
				counts := make(map[string]int)
				for _, pod := range podList.Items {
					counts[k8s.GetProxyVersion(pod)]++
				}
				if len(counts) == 0 {
					fmt.Fprintln(stdout, "Proxy versions: unavailable")
				} else {
					fmt.Fprintln(stdout, "Proxy versions:")
					for version, count := range counts {
						fmt.Fprintf(stdout, "\t%s (%d pods)\n", version, count)
					}
				}
			}
		}
	}
}
