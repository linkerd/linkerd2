// Package pcadelegate is used to delegate Certificate Signing Requests to AWS Private Certificate Authority.
package pcadelegate

import (
	"errors"
	"time"

	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/request"
	log "github.com/sirupsen/logrus"
)

type (

	// ACMPCARetryer is the interface that maps to AWS-SDK-GO's retry type.
	ACMPCARetryer interface {
		// MaxRetries represents maximum number of times a request retry.
		MaxRetries() int

		// RetryRules returns the duration to wait before retrying.
		RetryRules(r *request.Request) time.Duration

		// ShouldRetry deteremines if we should retry a request.
		ShouldRetry(r *request.Request) bool
	}

	// ACMPCARetry implements the ACMPCARetryer interface based on the AWS-SDK-GO retry type.
	ACMPCARetry struct {
		// This struct is composed on top of the AWS-SDK-GO DefaultRetryer.
		client.DefaultRetryer
	}
)

// NewACMPCARetry is a factory method used to construct an ACMPCARetry.
// It is configured with a number of retries. The number of retries must be larger than zero.
func NewACMPCARetry(maxRetries int) (*ACMPCARetry, error) {
	if maxRetries < 1 {
		return nil, errors.New("NewACMPCARetry was invoked with an invalid number of maxRetries. Please use a value greater than 0")
	}

	retry := ACMPCARetry{
		client.DefaultRetryer{
			NumMaxRetries: maxRetries,
		},
	}
	return &retry, nil
}

// ShouldRetry is used to determine if we should retry based on the request contents.
func (a ACMPCARetry) ShouldRetry(r *request.Request) bool {
	log.Infof("There was an error with status code %v with status %v\n", r.HTTPResponse.StatusCode, r.HTTPResponse.Status)
	// Error codes https://docs.aws.amazon.com/acm-pca/latest/APIReference/API_IssueCertificate.html
	// TODO check the specific 400 error code and don't retry if the CA is in a bad state.
	return 400 == r.HTTPResponse.StatusCode || a.DefaultRetryer.ShouldRetry(r)
}
