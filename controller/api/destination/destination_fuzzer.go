package destination

import (
	"testing"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/api/util"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	logging "github.com/sirupsen/logrus"
)

func init() {
	testing.Init()
}

// FuzzAdd fuzzes the Add method of the destination server.
func FuzzAdd(data []byte) int {
	f := fuzz.NewConsumer(data)
	set := watcher.AddressSet{}
	err := f.GenerateStruct(&set)
	if err != nil {
		return 0
	}
	t := &testing.T{}
	_, translator := makeEndpointTranslator(t)
	defer translator.Close()
	translator.Add(set)
	translator.Remove(set)
	return 1
}

// FuzzGet fuzzes the Get method of the destination server.
func FuzzGet(data []byte) int {
	f := fuzz.NewConsumer(data)
	dest1 := &pb.GetDestination{}
	err := f.GenerateStruct(dest1)
	if err != nil {
		return 0
	}
	dest2 := &pb.GetDestination{}
	err = f.GenerateStruct(dest2)
	if err != nil {
		return 0
	}
	dest3 := &pb.GetDestination{}
	err = f.GenerateStruct(dest3)
	if err != nil {
		return 0
	}
	t := &testing.T{}
	server := makeServer(t)

	stream := &bufferingGetStream{
		updates:          make(chan *pb.Update, 50),
		MockServerStream: util.NewMockServerStream(),
	}
	_ = server.Get(dest1, stream)
	_ = server.Get(dest2, stream)
	_ = server.Get(dest3, stream)
	return 1
}

// FuzzGetProfile fuzzes the GetProfile method of the destination server.
func FuzzGetProfile(data []byte) int {
	f := fuzz.NewConsumer(data)
	dest := &pb.GetDestination{}
	err := f.GenerateStruct(dest)
	if err != nil {
		return 0
	}
	t := &testing.T{}
	server := makeServer(t)
	stream := &bufferingGetProfileStream{
		updates:          []*pb.DestinationProfile{},
		MockServerStream: util.NewMockServerStream(),
	}
	stream.Cancel()
	_ = server.GetProfile(dest, stream)
	return 1
}

// FuzzProfileTranslatorUpdate fuzzes the Update method of the profile translator.
func FuzzProfileTranslatorUpdate(data []byte) int {
	f := fuzz.NewConsumer(data)
	profile := &sp.ServiceProfile{}
	err := f.GenerateStruct(profile)
	if err != nil {
		return 0
	}
	t := &testing.T{}

	id := watcher.ServiceID{Namespace: "bar", Name: "foo"}
	server := newMockDestinationGetProfileServer(50)
	translator, err := newProfileTranslator(id, server.profilesReceived, logging.WithField("test", t.Name()), "foo.bar.svc.cluster.local", 80, nil)
	if err != nil {
		return 0
	}
	defer translator.Close()
	translator.Update(profile)
	select {
	case <-server.profilesReceived:
	default:
	}
	return 1
}
