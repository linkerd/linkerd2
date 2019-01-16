// Copyright 2017 CNI authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/proxy-init/cmd"
	"github.com/linkerd/linkerd2/proxy-init/iptables"
	"github.com/projectcalico/libcalico-go/lib/logutils"
	"github.com/sirupsen/logrus"
)

var (
	injectAnnotationKey = "linkerd.io/setup-iptables"
)

// ProxyInit is the configuration for the proxy-init binary
type ProxyInit struct {
	IncomingProxyPort     int   `json:"incoming-proxy-port"`
	OutgoingProxyPort     int   `json:"outgoing-proxy-port"`
	ProxyUID              int   `json:"proxy-uid"`
	PortsToRedirect       []int `json:"ports-to-redirect"`
	InboundPortsToIgnore  []int `json:"inbound-ports-to-ignore"`
	OutboundPortsToIgnore []int `json:"outbound-ports-to-ignore"`
	Simulate              bool  `json:"simulate"`
}

// Kubernetes a K8s specific struct to hold config
type Kubernetes struct {
	K8sAPIRoot        string   `json:"k8s_api_root"`
	Kubeconfig        string   `json:"kubeconfig"`
	NodeName          string   `json:"node_name"`
	ExcludeNamespaces []string `json:"exclude_namespaces"`
}

// K8sArgs is the valid CNI_ARGS used for Kubernetes
// The field names need to match exact keys in kubelet args for unmarshalling
type K8sArgs struct {
	types.CommonArgs
	IP                         net.IP
	K8S_POD_NAME               types.UnmarshallableString
	K8S_POD_NAMESPACE          types.UnmarshallableString
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString
}

// PluginConf is whatever JSON is passed via stdin.
type PluginConf struct {
	types.NetConf

	// This is the previous result, when called in the context of a chained
	// plugin. We will just pass any prevResult through.
	RawPrevResult *map[string]interface{} `json:"prevResult"`
	PrevResult    *current.Result         `json:"-"`

	LogLevel   string     `json:"log_level"`
	ProxyInit  ProxyInit  `json:"linkerd2"`
	Kubernetes Kubernetes `json:"kubernetes"`
}

// parseConfig parses the supplied configuration (and prevResult) from stdin.
func parseConfig(stdin []byte) (*PluginConf, error) {
	conf := PluginConf{}

	logrus.Debugf("stdin to plugin: %v", string(stdin))
	if err := json.Unmarshal(stdin, &conf); err != nil {
		return nil, fmt.Errorf("failed to parse network configuration: %v", err)
	}

	// Begin previous result parsing
	if conf.RawPrevResult != nil {
		resultBytes, err := json.Marshal(conf.RawPrevResult)
		if err != nil {
			return nil, fmt.Errorf("could not serialize prevResult: %v", err)
		}
		logrus.Debugf("RawPrevResult: %v", string(resultBytes))

		res, err := version.NewResult(conf.CNIVersion, resultBytes)
		if err != nil {
			return nil, fmt.Errorf("could not parse prevResult: %v", err)
		}
		conf.RawPrevResult = nil
		conf.PrevResult, err = current.NewResultFromResult(res)
		if err != nil {
			return nil, fmt.Errorf("could not convert result to current version: %v", err)
		}
		logrus.Debugf("New PrevResult: %v", conf.PrevResult)
	}
	// End previous result parsing

	return &conf, nil
}

// ConfigureLogging sets up logging using the provided log level
func ConfigureLogging(logLevel string) {
	if strings.EqualFold(logLevel, "debug") {
		logrus.SetLevel(logrus.DebugLevel)
	} else if strings.EqualFold(logLevel, "info") {
		logrus.SetLevel(logrus.InfoLevel)
	} else {
		// Default level
		logrus.SetLevel(logrus.WarnLevel)
	}

	// Must log to Stderr because the CNI runtime uses Stdout as its state
	logrus.SetOutput(os.Stderr)
}

// cmdAdd is called by the CNI runtime for ADD requests
func cmdAdd(args *skel.CmdArgs) error {
	logrus.Info("cmdAdd parsing config")
	conf, err := parseConfig(args.StdinData)
	if err != nil {
		return err
	}
	ConfigureLogging(conf.LogLevel)

	if conf.PrevResult != nil {
		logrus.WithFields(logrus.Fields{
			"version":    conf.CNIVersion,
			"prevResult": conf.PrevResult,
		}).Info("cmdAdd config parsed")
	} else {
		logrus.WithFields(logrus.Fields{
			"version": conf.CNIVersion,
		}).Info("cmdAdd config parsed")
	}

	// Determine if running under k8s by checking the CNI args
	logrus.Infof("Getting identifiers with arguments: %s", args.Args)
	k8sArgs := K8sArgs{}
	if err := types.LoadArgs(args.Args, &k8sArgs); err != nil {
		return err
	}
	logrus.Infof("Loaded k8s arguments: %v", k8sArgs)

	var logEntry *logrus.Entry
	logEntry = logrus.WithFields(logrus.Fields{
		"ContainerID": args.ContainerID,
		"Pod":         string(k8sArgs.K8S_POD_NAME),
		"Namespace":   string(k8sArgs.K8S_POD_NAMESPACE),
	})

	// Check if running under Kubernetes.
	if string(k8sArgs.K8S_POD_NAMESPACE) != "" && string(k8sArgs.K8S_POD_NAME) != "" {
		excludePod := false
		for _, excludeNs := range conf.Kubernetes.ExcludeNamespaces {
			if string(k8sArgs.K8S_POD_NAMESPACE) == excludeNs {
				excludePod = true
				break
			}
		}
		if !excludePod {
			client, err := k8s.NewAPI(conf.Kubernetes.Kubeconfig, "linkerd2-cni-context")
			if err != nil {
				return err
			}
			logrus.WithField("client", client).Debug("Created Kubernetes client")

			httpClient, err := client.NewClient()
			if err != nil {
				return err
			}
			logrus.WithField("httpClient", httpClient).Debug("Created httpClient")

			pod, err := client.GetPodInNamespace(httpClient, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME))
			if err != nil {
				return err
			}
			logEntry.WithField("pod", pod).Debugf("Found Pod: %v in Namespace: %v", pod.GetName(), string(k8sArgs.K8S_POD_NAMESPACE))

			annotations := pod.GetAnnotations()
			containers := make([]string, len(pod.Spec.Containers))
			ports := make([]string, 4)
			for containerIdx, container := range pod.Spec.Containers {
				logEntry.WithFields(logrus.Fields{
					"container": container.Name,
				}).Debug("Inspecting container")
				containers[containerIdx] = container.Name

				if container.Name == "linkerd-proxy" {
					// don't include ports from linkerd-proxy in the redirect ports
					continue
				}
				for _, containerPort := range container.Ports {
					logEntry.WithFields(logrus.Fields{
						"container": container.Name,
						"port":      containerPort,
					}).Debug("Added pod port")

					ports = append(ports, strconv.Itoa(int(containerPort.ContainerPort)))
				}
			}

			logEntry.Infof("Found containers %v", containers)
			if len(containers) > 1 {
				logEntry.WithFields(logrus.Fields{
					"netns":       args.Netns,
					"ports":       ports,
					"annotations": annotations,
				}).Infof("Checking annotations prior to redirect for linkerd-proxy")
				if val, ok := annotations[injectAnnotationKey]; ok {
					logEntry.Infof("Pod %s contains inject annotation: %s", string(k8sArgs.K8S_POD_NAME), val)
					if injectEnabled, err := strconv.ParseBool(val); err == nil {
						if !injectEnabled {
							logEntry.Infof("Pod excluded due to inject-disabled annotation")
							excludePod = true
						}
					}
				}
				if !excludePod {
					logEntry.Infof("setting up iptables firewall")
					options := cmd.RootOptions{
						IncomingProxyPort:     conf.ProxyInit.IncomingProxyPort,
						OutgoingProxyPort:     conf.ProxyInit.OutgoingProxyPort,
						ProxyUserID:           conf.ProxyInit.ProxyUID,
						PortsToRedirect:       conf.ProxyInit.PortsToRedirect,
						InboundPortsToIgnore:  conf.ProxyInit.InboundPortsToIgnore,
						OutboundPortsToIgnore: conf.ProxyInit.OutboundPortsToIgnore,
						SimulateOnly:          conf.ProxyInit.Simulate,
						NetNs:                 args.Netns,
					}
					logEntry.Debugf("options being passed: %v", options)
					firewallConfiguration, err := cmd.BuildFirewallConfiguration(&options)
					logEntry.Debugf("firewallConfiguration: %v", firewallConfiguration)
					if err != nil {
						logEntry.Errorf("Could not create a Firewall Configuration from the options: %v", options)
						return err
					}
					iptables.ConfigureFirewall(*firewallConfiguration)
				}
			}
		} else {
			logEntry.Infof("Pod excluded")
		}
	} else {
		logEntry.Infof("No Kubernetes Data")
	}

	logrus.Infof("plugin is finished")
	if conf.PrevResult != nil {
		// Pass through the prevResult for the next plugin
		logrus.Debugf("Passing previous result: %v\n\n%v", conf.PrevResult, conf.PrevResult.String())
		return types.PrintResult(conf.PrevResult, conf.CNIVersion)
	}

	logrus.Infof("emptying stdout")
	return nil
}

// cmdDel is called for DELETE requests
func cmdDel(args *skel.CmdArgs) error {
	logrus.Info("cmdDel parsing config")
	conf, err := parseConfig(args.StdinData)
	if err != nil {
		return err
	}
	ConfigureLogging(conf.LogLevel)
	_ = conf

	// Do your delete here

	return nil
}

func main() {
	// Set up logging formatting.
	logrus.SetFormatter(&logutils.Formatter{})

	// Install a hook that adds file/line no information.
	logrus.AddHook(&logutils.ContextHook{})

	skel.PluginMain(cmdAdd, cmdDel, version.All)
}

func cmdGet(args *skel.CmdArgs) error {
	logrus.Info("cmdGet not implemented")
	return fmt.Errorf("not implemented")
}
