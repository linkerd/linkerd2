package config

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	pb "github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/k8s"
)

const configMapName = "linkerd-config"

var unmarshaler = jsonpb.Unmarshaler{}

// Global returns the Global protobuf config from the linkerd-config ConfigMap
func Global() (*pb.Global, error) {
	config := &pb.Global{}
	err := unmarshalConfig(k8s.MountPathGlobalConfig, config)
	return config, err
}

// Proxy returns the Proxy protobuf config from the linkerd-config ConfigMap
func Proxy() (*pb.Proxy, error) {
	config := &pb.Proxy{}
	err := unmarshalConfig(k8s.MountPathProxyConfig, config)
	return config, err
}

func unmarshalConfig(filepath string, msg proto.Message) error {
	configJSON, err := ioutil.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("Error reading config: %s", err)
	}

	log.Debugf("%s config JSON: %s", filepath, configJSON)

	err = unmarshaler.Unmarshal(bytes.NewReader(configJSON), msg)
	if err != nil {
		return fmt.Errorf("Error unmarshaling config: %s", err)
	}

	return nil
}

// Fetch the configuration from the kubernetes API.
func Fetch(k corev1.ConfigMapInterface) (global *pb.Global, proxy *pb.Proxy, err error) {
	cm, err := k.Get(configMapName, metav1.GetOptions{})
	if err != nil {
		return
	}

	if j := cm.Data["global"]; j != "" {
		global = &pb.Global{}
		if err = unmarshaler.Unmarshal(strings.NewReader(j), global); err != nil {
			return
		}
	}

	if j := cm.Data["proxy"]; j != "" {
		proxy = &pb.Proxy{}
		if err = unmarshaler.Unmarshal(strings.NewReader(j), proxy); err != nil {
			return
		}
	}

	return
}
