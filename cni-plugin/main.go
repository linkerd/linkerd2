// Copyright 2017 CNI authors
// Modifications copyright (c) Linkerd authors
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

// This file was inspired by:
// 1) https://github.com/istio/cni/blob/c63a509539b5ed165a6617548c31b686f13c2133/cmd/istio-cni/main.go

package main

import (
	"encoding/json"
	"fmt"
	"os"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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
	K8sAPIRoot string `json:"k8s_api_root"`
	Kubeconfig string `json:"kubeconfig"`
}

// K8sArgs is the valid CNI_ARGS used for Kubernetes
// The field names need to match exact keys in kubelet args for unmarshalling
type K8sArgs struct {
	types.CommonArgs
	K8sPodName      types.UnmarshallableString
	K8sPodNamespace types.UnmarshallableString
}

// PluginConf is whatever JSON is passed via stdin.
type PluginConf struct {
	types.NetConf

	// This is the previous result, when called in the context of a chained
	// plugin. We will just pass any prevResult through.
	RawPrevResult *map[string]interface{} `json:"prevResult"`
	PrevResult    *current.Result         `json:"-"`

	LogLevel   string     `json:"log_level"`
	ProxyInit  ProxyInit  `json:"linkerd"`
	Kubernetes Kubernetes `json:"kubernetes"`
}

func main() {
	// Set up logging formatting.
	logrus.SetFormatter(&logutils.Formatter{})
	// Install a hook that adds file/line no information.
	logrus.AddHook(&logutils.ContextHook{})
	skel.PluginMain(cmdAdd, cmdDel, version.All)
}

func configureLogging(logLevel string) {
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

// parseConfig parses the supplied configuration (and prevResult) from stdin.
func parseConfig(stdin []byte) (*PluginConf, error) {
	conf := PluginConf{}

	logrus.Debugf("linkerd-cni: stdin to plugin: %v", string(stdin))
	if err := json.Unmarshal(stdin, &conf); err != nil {
		return nil, fmt.Errorf("linkerd-cni: failed to parse network configuration: %v", err)
	}

	if conf.RawPrevResult != nil {
		resultBytes, err := json.Marshal(conf.RawPrevResult)
		if err != nil {
			return nil, fmt.Errorf("linkerd-cni: could not serialize prevResult: %v", err)
		}

		res, err := version.NewResult(conf.CNIVersion, resultBytes)
		if err != nil {
			return nil, fmt.Errorf("linkerd-cni: could not parse prevResult: %v", err)
		}
		conf.RawPrevResult = nil
		conf.PrevResult, err = current.NewResultFromResult(res)
		if err != nil {
			return nil, fmt.Errorf("linkerd-cni: could not convert result to current version: %v", err)
		}
		logrus.Debugf("linkerd-cni: prevResult: %v", conf.PrevResult)
	}

	return &conf, nil
}

// cmdAdd is called by the CNI runtime for ADD requests
func cmdAdd(args *skel.CmdArgs) error {
	logrus.Debug("linkerd-cni: cmdAdd, parsing config")
	conf, err := parseConfig(args.StdinData)
	if err != nil {
		return err
	}
	configureLogging(conf.LogLevel)

	if conf.PrevResult != nil {
		logrus.WithFields(logrus.Fields{
			"version":    conf.CNIVersion,
			"prevResult": conf.PrevResult,
		}).Debug("linkerd-cni: cmdAdd, config parsed")
	} else {
		logrus.WithFields(logrus.Fields{
			"version": conf.CNIVersion,
		}).Debug("linkerd-cni: cmdAdd, config parsed")
	}

	// Determine if running under k8s by checking the CNI args
	k8sArgs := K8sArgs{}
	args.Args = strings.Replace(args.Args, "K8S_POD_NAMESPACE", "K8sPodNamespace", 1)
	args.Args = strings.Replace(args.Args, "K8S_POD_NAME", "K8sPodName", 1)
	if err := types.LoadArgs(args.Args, &k8sArgs); err != nil {
		return err
	}

	namespace := string(k8sArgs.K8sPodNamespace)
	podName := string(k8sArgs.K8sPodName)
	logEntry := logrus.WithFields(logrus.Fields{
		"ContainerID": args.ContainerID,
		"Pod":         podName,
		"Namespace":   namespace,
	})

	if namespace != "" && podName != "" {
		config, err := k8s.GetConfig(conf.Kubernetes.Kubeconfig, "linkerd-cni-context")
		if err != nil {
			return err
		}

		client, err := kubernetes.NewForConfig(config)
		if err != nil {
			return err
		}

		pod, err := client.CoreV1().Pods(namespace).Get(podName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		containsLinkerdProxy := false
		for _, container := range pod.Spec.Containers {
			if container.Name == k8s.ProxyContainerName {
				containsLinkerdProxy = true
				break
			}
		}

		containsInitContainer := false
		for _, container := range pod.Spec.InitContainers {
			if container.Name == k8s.InitContainerName {
				containsInitContainer = true
				break
			}
		}

		if containsLinkerdProxy && !containsInitContainer {
			logEntry.Infof("linkerd-cni: setting up iptables firewall")
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
			firewallConfiguration, err := cmd.BuildFirewallConfiguration(&options)
			if err != nil {
				logEntry.Errorf("linkerd-cni: could not create a Firewall Configuration from the options: %v", options)
				return err
			}
			iptables.ConfigureFirewall(*firewallConfiguration)
		} else {
			if containsInitContainer {
				logEntry.Infof("linkerd-cni: linkerd-init initContainer is present, skipping.")
			} else {
				logEntry.Infof("linkerd-cni: linkerd-proxy is not present, skipping.")
			}
		}
	} else {
		logEntry.Infof("linkerd-cni: no Kubernetes namespace or pod name found, skipping.")
	}

	logrus.Infof("linkerd-cni: plugin is finished")
	if conf.PrevResult != nil {
		// Pass through the prevResult for the next plugin
		return types.PrintResult(conf.PrevResult, conf.CNIVersion)
	}

	logrus.Infof("linkerd-cni: no previous result to pass through, emptying stdout")
	return nil
}

// cmdDel is called for DELETE requests
func cmdDel(args *skel.CmdArgs) error {
	logrus.Info("linkerd-cni: cmdDel not implemented")
	return nil
}
