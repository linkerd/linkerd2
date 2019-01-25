package proxy

import (
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	"github.com/linkerd/linkerd2/pkg/profiles"
	log "github.com/sirupsen/logrus"
)

type profileUpdateListener interface {
	Update(profile *sp.ServiceProfile)
	ClientClose() <-chan struct{}
	ServerClose() <-chan struct{}
	Stop()
}

// implements the profileUpdateListener interface
type profileListener struct {
	stream pb.Destination_GetProfileServer
	stopCh chan struct{}
}

func newProfileListener(stream pb.Destination_GetProfileServer) *profileListener {
	return &profileListener{
		stream: stream,
		stopCh: make(chan struct{}),
	}
}

func (l *profileListener) ClientClose() <-chan struct{} {
	return l.stream.Context().Done()
}

func (l *profileListener) ServerClose() <-chan struct{} {
	return l.stopCh
}

func (l *profileListener) Stop() {
	close(l.stopCh)
}

func (l *profileListener) Update(profile *sp.ServiceProfile) {
	if profile != nil {
		destinationProfile, err := profiles.ToServiceProfile(&profile.Spec)
		if err != nil {
			log.Error(err)
			return
		}
		log.Debugf("%s: %+v", profile.GetName(), *destinationProfile)
		l.stream.Send(destinationProfile)
	}
}
