package cmd

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/linkerd/linkerd2/controller/gen/config"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"
)

// These constants are used by the `show` flag.
const (
	// showLinkerd opens the Linkerd dashboard in a web browser (default).
	showLinkerd = "linkerd"

	// showGrafana opens the Grafana dashboard in a web browser.
	showGrafana = "grafana"

	// showURL displays dashboard URLs without opening a browser.
	showURL = "url"

	// webDeployment is the name of the web deployment in cli/install/template.go
	webDeployment = "linkerd-web"

	// webPort is the http port from the web pod spec in cli/install/template.go
	webPort = 8084

	// defaultHost is the default host used for port-forwarding via `linkerd dashboard`
	defaultHost = "localhost"

	// defaultPort is the port the user will point his browser to
	defaultPort = 50750

	// targetPort is the port where the k8s' port-forward will be listening to
	targetPort = 50760
)

type dashboardOptions struct {
	host string
	port int
	show string
	wait time.Duration
}

func newDashboardOptions() *dashboardOptions {
	return &dashboardOptions{
		host: defaultHost,
		port: defaultPort,
		show: showLinkerd,
		wait: 300 * time.Second,
	}
}

func newCmdDashboard() *cobra.Command {
	options := newDashboardOptions()

	cmd := &cobra.Command{
		Use:   "dashboard [flags]",
		Short: "Open the Linkerd dashboard in a web browser",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.port < 0 {
				return fmt.Errorf("port must be greater than or equal to zero, was %d", options.port)
			}

			if options.show != showLinkerd && options.show != showGrafana && options.show != showURL {
				return fmt.Errorf("unknown value for 'show' param, was: %s, must be one of: %s, %s, %s",
					options.show, showLinkerd, showGrafana, showURL)
			}

			// ensure we can connect to the public API before starting the proxy
			apiClient := checkPublicAPIClientOrRetryOrExit(time.Now().Add(options.wait), true)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			config, err := apiClient.Config(ctx, &pb.Empty{})
			if err != nil {
				return err
			}

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, 0)
			if err != nil {
				return err
			}

			signals := make(chan os.Signal, 1)
			signal.Notify(signals, os.Interrupt)
			defer signal.Stop(signals)

			portforward, err := k8s.NewPortForward(
				k8sAPI,
				controlPlaneNamespace,
				webDeployment,
				options.host,
				targetPort,
				webPort,
				verbose,
			)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to initialize port-forward: %s\n", err)
				os.Exit(1)
			}
			if options.port == 0 {
				options.port, err = k8s.GetEphemeralPort()
				if err != nil {
					return err
				}
			}

			if err = portforward.Init(); err != nil {
				// TODO: consider falling back to an ephemeral port if defaultPort is taken
				fmt.Fprintf(os.Stderr, "Error running port-forward: %s\nCheck for `linkerd dashboard` running in other terminal sessions, or use the `--port` flag.\n", err)
				os.Exit(1)
			}

			go func() {
				<-signals
				portforward.Stop()
			}()

			webURL := fmt.Sprintf("http://%s", setupLocalServer(config, options))
			grafanaURL := fmt.Sprintf("%s/grafana", webURL)

			fmt.Printf("Linkerd dashboard available at:\n%s\n", webURL)
			fmt.Printf("Grafana dashboard available at:\n%s\n", grafanaURL)

			switch options.show {
			case showLinkerd:
				fmt.Println("Opening Linkerd dashboard in the default browser")

				err = browser.OpenURL(webURL)
				if err != nil {
					fmt.Fprintln(os.Stderr, "Failed to open Linkerd dashboard automatically")
					fmt.Fprintf(os.Stderr, "Visit %s in your browser to view the dashboard\n", webURL)
				}
			case showGrafana:
				fmt.Println("Opening Grafana dashboard in the default browser")

				err = browser.OpenURL(grafanaURL)
				if err != nil {
					fmt.Fprintln(os.Stderr, "Failed to open Grafana dashboard automatically")
					fmt.Fprintf(os.Stderr, "Visit %s in your browser to view the dashboard\n", grafanaURL)
				}
			case showURL:
				// no-op, we already printed the URLs
			}

			<-portforward.GetStop()
			return nil
		},
	}

	// This is identical to what `kubectl proxy --help` reports, `--port 0` indicates a random port.
	cmd.PersistentFlags().StringVar(&options.host, "address", options.host, "The address at which to serve requests")
	cmd.PersistentFlags().IntVarP(&options.port, "port", "p", options.port, "The local port on which to serve requests (when set to 0, a random port will be used)")
	cmd.PersistentFlags().StringVar(&options.show, "show", options.show, "Open a dashboard in a browser or show URLs in the CLI (one of: linkerd, grafana, url)")
	cmd.PersistentFlags().DurationVar(&options.wait, "wait", options.wait, "Wait for dashboard to become available if it's not available when the command is run")

	return cmd
}

// setupLocalServer creates a web server listening on
// http://$options.host:$options.port (host and port can be set as CLI params)
// that forwards the connection to the k8s port-forwarder listening on
// http://$options.host:$targetPort (targetPort is fixed).
// Any request received with the Host header different than $options.host:$options.port
// will be rejected, thus preventing DNS rebinding attacks (see #3083).
// This local web server will also rewrite the Host header to something like
// "linkerd-web.linkerd.svc.cluster.local:8084" required by the linkerd-web service.
func setupLocalServer(config *config.All, options *dashboardOptions) string {
	localhost := fmt.Sprintf("%s:%d", options.host, options.port)

	go func() {
		rpURL, err := url.Parse(fmt.Sprintf("http://%s:%d", options.host, targetPort))
		if err != nil {
			log.Fatal(err)
		}

		targetHost := fmt.Sprintf(
			"%s.%s.svc.%s:%d",
			webDeployment,
			config.GetGlobal().GetLinkerdNamespace(),
			config.GetGlobal().GetClusterDomain(),
			webPort,
		)
		proxy := newSingleHostReverseProxy(targetHost, rpURL)

		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.Host != localhost {
				http.Error(w, "Invalid Host header", http.StatusNotFound)
				return
			}
			proxy.ServeHTTP(w, r)
		})
		log.Fatal(http.ListenAndServe(localhost, nil))
	}()

	return localhost
}

// newSingleHostReverseProxy is modeled after http.httputil.NewSingleHostReverseProxy
// using a custom Director that rewrites the Host and Origin headers
func newSingleHostReverseProxy(host string, target *url.URL) *httputil.ReverseProxy {
	director := func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		if _, ok := req.Header["User-Agent"]; !ok {
			// explicitly disable User-Agent so it's not set to default value
			req.Header.Set("User-Agent", "")
		}
		req.Host = host
		req.Header.Set("Origin", fmt.Sprintf("http://%s", host))
	}
	return &httputil.ReverseProxy{Director: director}
}
