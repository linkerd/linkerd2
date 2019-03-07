package config

import (
	"bytes"
	"io/ioutil"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	pb "github.com/linkerd/linkerd2/controller/gen/config"
	log "github.com/sirupsen/logrus"
)

var unmarshaler = jsonpb.Unmarshaler{}

// Global returns the Global protobuf config from the linkerd-config ConfigMap
func Global(filepath string) (*pb.Global, error) {
	config := &pb.Global{}
	err := unmarshalConfig(filepath, config)
	return config, err
}

// Proxy returns the Proxy protobuf config from the linkerd-config ConfigMap
func Proxy(filepath string) (*pb.Proxy, error) {
	config := &pb.Proxy{}
	err := unmarshalConfig(filepath, config)
	return config, err
}

func unmarshalConfig(filepath string, msg proto.Message) error {
	configJSON, err := ioutil.ReadFile(filepath)
	if err != nil {
		log.Errorf("error reading %s: %s", filepath, err)
		return err
	}

	log.Debugf("%s config JSON: %s", filepath, configJSON)

	err = unmarshaler.Unmarshal(bytes.NewReader(configJSON), msg)
	if err != nil {
		log.Errorf("error unmarshaling %s: %s", filepath, err)
		return err
	}

	return nil
}
