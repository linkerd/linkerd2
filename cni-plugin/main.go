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

// This is a sample chained plugin that supports multiple CNI versions. It
// parses prevResult according to the cniVersion
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
	"github.com/linkerd/linkerd2/proxy-init/cmd"
	"github.com/linkerd/linkerd2/proxy-init/iptables"
	"github.com/projectcalico/libcalico-go/lib/logutils"
	"github.com/sirupsen/logrus"
)

var (
	nsSetupBinDir       = "/opt/cni/bin"
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
	CniBinDir         string   `json:"cni_bin_dir"`
}

// K8sArgs is the valid CNI_ARGS used for Kubernetes
// The field names need to match exact keys in kubelet args for unmarshalling
type K8sArgs struct {
	types.CommonArgs
	IP                         net.IP
	K8S_POD_NAME               types.UnmarshallableString // nolint: golint
	K8S_POD_NAMESPACE          types.UnmarshallableString // nolint: golint
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString // nolint: golint
}

// PluginConf is whatever you expect your configuration json to be. This is whatever
// is passed in on stdin. Your plugin may wish to expose its functionality via
// runtime args, see CONVENTIONS.md in the CNI spec.
type PluginConf struct {
	types.NetConf // You may wish to not nest this type
	RuntimeConfig *struct {
		SampleConfig map[string]interface{} `json:"sample"`
	} `json:"runtimeConfig"`

	// This is the previous result, when called in the context of a chained
	// plugin. Because this plugin supports multiple versions, we'll have to
	// parse this in two passes. If your plugin is not chained, this can be
	// removed (though you may wish to error if a non-chainable plugin is
	// chained.
	// If you need to modify the result before returning it, you will need
	// to actually convert it to a concrete versioned struct.
	RawPrevResult *map[string]interface{} `json:"prevResult"`
	PrevResult    *current.Result         `json:"-"`

	// Add plugin-specifc flags here
	LogLevel   string     `json:"log_level"`
	ProxyInit  ProxyInit  `json:"proxy-init"`
	Kubernetes Kubernetes `json:"kubernetes"`
}

// parseConfig parses the supplied configuration (and prevResult) from stdin.
func parseConfig(stdin []byte) (*PluginConf, error) {
	conf := PluginConf{}

	if err := json.Unmarshal(stdin, &conf); err != nil {
		return nil, fmt.Errorf("failed to parse network configuration: %v", err)
	}

	// Parse previous result. Remove this if your plugin is not chained.
	if conf.RawPrevResult != nil {
		resultBytes, err := json.Marshal(conf.RawPrevResult)
		if err != nil {
			return nil, fmt.Errorf("could not serialize prevResult: %v", err)
		}
		res, err := version.NewResult(conf.CNIVersion, resultBytes)
		if err != nil {
			return nil, fmt.Errorf("could not parse prevResult: %v", err)
		}
		conf.RawPrevResult = nil
		conf.PrevResult, err = current.NewResultFromResult(res)
		if err != nil {
			return nil, fmt.Errorf("could not convert result to current version: %v", err)
		}
	}
	// End previous result parsing

	// Do any validation here

	return &conf, nil
}

// ConfigureLogging sets up logging using the provided log level,
func ConfigureLogging(logLevel string) {
	if strings.EqualFold(logLevel, "debug") {
		logrus.SetLevel(logrus.DebugLevel)
	} else if strings.EqualFold(logLevel, "info") {
		logrus.SetLevel(logrus.InfoLevel)
	} else {
		// Default level
		logrus.SetLevel(logrus.WarnLevel)
	}

	logrus.SetOutput(os.Stderr)
}

// cmdAdd is called for ADD requests
func cmdAdd(args *skel.CmdArgs) error {
	logrus.Info("linkerd2-cni cmdAdd parsing config")
	conf, err := parseConfig(args.StdinData)
	if err != nil {
		return err
	}
	ConfigureLogging(conf.LogLevel)

	if conf.PrevResult == nil {
		logrus.Error("must be called as chained plugin")
		return fmt.Errorf("must be called as chained plugin")
	}

	logrus.WithFields(logrus.Fields{
		"version":    conf.CNIVersion,
		"prevResult": conf.PrevResult,
	}).Info("cmdAdd config parsed")

	// Determine if running under k8s by checking the CNI args
	k8sArgs := K8sArgs{}
	if err := types.LoadArgs(args.Args, &k8sArgs); err != nil {
		return err
	}
	logrus.Infof("Getting identifiers with arguments: %s", args.Args)
	logrus.Infof("Loaded k8s arguments: %v", k8sArgs)
	if conf.Kubernetes.CniBinDir != "" {
		nsSetupBinDir = conf.Kubernetes.CniBinDir
	}

	var logger *logrus.Entry
	logger = logrus.WithFields(logrus.Fields{
		"ContainerID": args.ContainerID,
		"Pod":         string(k8sArgs.K8S_POD_NAME),
		"Namespace":   string(k8sArgs.K8S_POD_NAMESPACE),
	})

	// Check if the workload is running under Kubernetes.
	if string(k8sArgs.K8S_POD_NAMESPACE) != "" && string(k8sArgs.K8S_POD_NAME) != "" {
		excludePod := false
		for _, excludeNs := range conf.Kubernetes.ExcludeNamespaces {
			if string(k8sArgs.K8S_POD_NAMESPACE) == excludeNs {
				excludePod = true
				break
			}
		}
		if !excludePod {
			client, err := newKubeClient(*conf, logger)
			if err != nil {
				return err
			}
			logrus.WithField("client", client).Debug("Created Kubernetes client")
			containers, _, annotations, ports, k8sErr := getKubePodInfo(client, string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE))
			if k8sErr != nil {
				logger.Warnf("Error geting Pod data %v", k8sErr)
			}
			logger.Infof("Found containers %v", containers)
			if len(containers) > 1 {
				logrus.WithFields(logrus.Fields{
					"ContainerID": args.ContainerID,
					"netns":       args.Netns,
					"pod":         string(k8sArgs.K8S_POD_NAME),
					"Namespace":   string(k8sArgs.K8S_POD_NAMESPACE),
					"ports":       ports,
					"annotations": annotations,
				}).Infof("Checking annotations prior to redirect for linkerd2-proxy")
				if val, ok := annotations[injectAnnotationKey]; ok {
					logrus.Infof("Pod %s contains inject annotation: %s", string(k8sArgs.K8S_POD_NAME), val)
					if injectEnabled, err := strconv.ParseBool(val); err == nil {
						if !injectEnabled {
							logrus.Infof("Pod excluded due to inject-disabled annotation")
							excludePod = true
						}
					}
				}
				if !excludePod {
					logrus.Infof("setting up iptables firewall")
					options := cmd.RootOptions{
						IncomingProxyPort:     conf.ProxyInit.IncomingProxyPort,
						OutgoingProxyPort:     conf.ProxyInit.OutgoingProxyPort,
						ProxyUserID:           conf.ProxyInit.ProxyUID,
						PortsToRedirect:       conf.ProxyInit.PortsToRedirect,
						InboundPortsToIgnore:  conf.ProxyInit.InboundPortsToIgnore,
						OutboundPortsToIgnore: conf.ProxyInit.OutboundPortsToIgnore,
						SimulateOnly:          conf.ProxyInit.Simulate,
					}
					firewallConfiguration, err := cmd.BuildFirewallConfiguration(&options)
					if err != nil {
						logger.Errorf("Could not create a Firewall Configuration from the options: %v", options)
						return err
					}
					iptables.ConfigureFirewall(*firewallConfiguration)
				}
			}
		} else {
			logger.Infof("Pod excluded")
		}
	} else {
		logger.Infof("No Kubernetes Data")
	}

	// Pass through the result for the next plugin
	return types.PrintResult(conf.PrevResult, conf.CNIVersion)
}

// cmdDel is called for DELETE requests
func cmdDel(args *skel.CmdArgs) error {
	logrus.Info("linkerd2-cni cmdDel parsing config")
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

	skel.PluginMain(cmdAdd, cmdGet, cmdDel, version.All, "CNI plugin linkerd2 v1.0.0")
}

func cmdGet(args *skel.CmdArgs) error {
	logrus.Info("cmdGet not implemented")
	// TODO: implement
	return fmt.Errorf("not implemented")
}
