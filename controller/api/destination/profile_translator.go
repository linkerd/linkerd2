package destination

import (
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	"github.com/linkerd/linkerd2/pkg/profiles"
	logging "github.com/sirupsen/logrus"
)

// implements the ProfileUpdateListener interface
type profileTranslator struct {
	stream pb.Destination_GetProfileServer
	log    *logging.Entry
}

func newProfileTranslator(stream pb.Destination_GetProfileServer, log *logging.Entry) *profileTranslator {
	return &profileTranslator{
		stream: stream,
		log:    log.WithField("component", "profile-translator"),
	}
}

func (pt *profileTranslator) Update(profile *sp.ServiceProfile) {
	if profile == nil {
		pt.stream.Send(&profiles.DefaultServiceProfile)
		return
	}
	destinationProfile, err := profiles.ToServiceProfile(profile)
	if err != nil {
		pt.log.Error(err)
		return
	}
	pt.log.Debugf("Sending profile update: %+v", destinationProfile)
	pt.stream.Send(destinationProfile)
}
