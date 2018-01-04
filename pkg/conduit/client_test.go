package conduit

import (
	"context"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"testing"

	pb "github.com/runconduit/conduit/controller/gen/public"
)

type mockTransport struct {
	responseToReturn *http.Response
	requestSent      *http.Request
	errorToReturn    error
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.requestSent = req
	return m.responseToReturn, m.errorToReturn
}

func TestNewInternalClient(t *testing.T) {
	t.Run("Makes a well-formed request over the Kubernetes public API", func(t *testing.T) {
		mockTransport := &mockTransport{}
		mockTransport.responseToReturn = &http.Response{
			StatusCode: 500,
			Body:       ioutil.NopCloser(strings.NewReader("body")),
		}
		mockHttpClient := &http.Client{
			Transport: mockTransport,
		}

		apiURL := &url.URL{
			Scheme: "http",
			Host:   "some-hostname",
			Path:   "/",
		}

		client, err := newClient(apiURL, mockHttpClient)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		_, err = client.Version(context.Background(), &pb.Empty{})

		expectedUrlRequested := "http://some-hostname/api/v1/Version"
		actualUrlRequested := mockTransport.requestSent.URL.String()
		if actualUrlRequested != expectedUrlRequested {
			t.Fatalf("Expected request to URL [%v], but got [%v]", expectedUrlRequested, actualUrlRequested)
		}
	})
}
