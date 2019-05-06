// Package pcadelegate is used to delegate Certificate Signing Requests to AWS Private Certificate Authority.
// IssueCertificate requests sent to the Identity service may use this instead of the local ca.
package pcadelegate

import (
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/service/acmpca"
)

// TestConvertNanoSecondsToDaysWholeDay is a unit test that validates that we correctly turn exactly three days worth of nanoseconds into 3 days
func TestConvertNanoSecondsToDaysWholeDay(t *testing.T) {
	threeDaysInNanoSeconds := 3 * (time.Hour * 24)
	var expectedResult int64 = 3
	result := ConvertNanoSecondsToDays(threeDaysInNanoSeconds)
	if result != expectedResult {
		t.Errorf("TestConvertNanoSecondsToDaysWholeDay failed. result = %v, expectedResult = %v", result, expectedResult)
	}
}

// TestConvertNanoSecondsToDaysLessThanWholeDay is a unit test that validates that floor(computedDays) to a integer number of days
func TestConvertNanoSecondsToDaysLessThanWholeDay(t *testing.T) {
	lessThanThreeDaysInNanoSeconds := 3*(time.Hour*24) - time.Hour
	// Any elements less than 3 days and more than 2 days will result in 2 days
	var expectedResult int64 = 2
	result := ConvertNanoSecondsToDays(lessThanThreeDaysInNanoSeconds)
	if result != expectedResult {
		t.Errorf("TestConvertNanoSecondsToDaysLessThanWholeDay failed. result = %v, expectedResult = %v", result, expectedResult)
	}
}

// TestSucessfulIssueEndCert is a unit test that validates correct behavior when GetCertificate, IssueCertificate, and certificate parsing succeed.
func TestSuccessfulIssueEndCert(t *testing.T) {
	issueCertARN := "myCertificateARN"
	issueCertResponse := acmpca.IssueCertificateOutput{
		CertificateArn: &issueCertARN,
	}

	endCert := "-----BEGIN CERTIFICATE-----\nMIIDqTCCApGgAwIBAgIRAPAIqwlKObt2rkcCLJOOFDgwDQYJKoZIhvcNAQELBQAw\nRTEVMBMGA1UECgwMQVdTX1BDQV9URVNUMRUwEwYDVQQLDAxBV1NfUENBX1RFU1Qx\nFTATBgNVBAMMDEFXU19QQ0FfVEVTVDAeFw0xOTA0MjkyMTA1MjJaFw0xOTA1Mjky\nMjA1MjJaMFMxUTBPBgNVBAMTSGxpbmtlcmQtY29udHJvbGxlci5saW5rZXJkLnNl\ncnZpY2VhY2NvdW50LmlkZW50aXR5LmxpbmtlcmQuY2x1c3Rlci5sb2NhbDBZMBMG\nByqGSM49AgEGCCqGSM49AwEHA0IABMY5fzpLWi1zbkV+7yTrrXyz+15aTnhOCnoc\nzx0Ty82qupXvwRqwWEx3JHT8gQrl4FcbxJ5+7FpoedW0BHKsHUejggFPMIIBSzBT\nBgNVHREETDBKgkhsaW5rZXJkLWNvbnRyb2xsZXIubGlua2VyZC5zZXJ2aWNlYWNj\nb3VudC5pZGVudGl0eS5saW5rZXJkLmNsdXN0ZXIubG9jYWwwCQYDVR0TBAIwADAf\nBgNVHSMEGDAWgBQGffYJi4qJPAFExizK60NN+IbbPTAdBgNVHQ4EFgQUbaYAI5Gl\nNFimZ0miBmXg3jkSDzwwDgYDVR0PAQH/BAQDAgWgMB0GA1UdJQQWMBQGCCsGAQUF\nBwMBBggrBgEFBQcDAjB6BgNVHR8EczBxMG+gbaBrhmlodHRwOi8vY2FwaW5vLXRl\nc3QtcHJpdmF0ZS1jYS1jcmwuczMudXMtd2VzdC0yLmFtYXpvbmF3cy5jb20vY3Js\nLzZlZTY0NWY2LTU0MGYtNDdiMS1hOWMzLWI1ZDA1YzEyNzkwYy5jcmwwDQYJKoZI\nhvcNAQELBQADggEBADSEaQxDHeQ3zY+VtGEWy8iiq/3FqEAAupzMjITphfl+ML0I\nVC9AfbMYbfkEu/y7D78kpO7jPOMpEZWMn84LOn7aIh5KM5dLKUj3lO7uGCfqyCaV\naKMjezUpR1DFd+/Dze7SXZPXuLvnAuv/otYMaqj+aq79t2LgJzmcvJRlH5e5P2sp\nvrut1H0Twh+Elsj5uKvNGBk90x72fiy8yYodUbUM2it2ifX/BbdStfj0qWj7gas9\nRhImS5tTT9rPS1pkZOps/448ruYG2eZUMDcqg/l8+ZqyZf9NU7kaNWEldWcADIwC\ngthCBJF4Wvc//wCYo6a6H2+k6j0DfJ20hnKG+/k=\n-----END CERTIFICATE-----"
	certChain := "-----BEGIN CERTIFICATE-----\nMIIDcjCCAlqgAwIBAgICEAwwDQYJKoZIhvcNAQELBQAwcDELMAkGA1UEBhMCVVMx\nCzAJBgNVBAgMAldBMRAwDgYDVQQHDAdTZWF0dGxlMQ4wDAYDVQQKDAVUTUVTSDEO\nMAwGA1UECwwFVE1FU0gxIjAgBgkqhkiG9w0BCQEWE1RNRVNIQG5vcmRzdHJvbS5j\nb20wHhcNMTkwNDI5MTgxMDU1WhcNMjAwNDI4MTgxMDU1WjBFMRUwEwYDVQQKDAxB\nV1NfUENBX1RFU1QxFTATBgNVBAsMDEFXU19QQ0FfVEVTVDEVMBMGA1UEAwwMQVdT\nX1BDQV9URVNUMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAovr/8Mjt\nbQaq85K7XsH35ZLrUTXZ9BoWPrY4tMKAQ3IqWlQz9SSja4TX9t1h1B8+k15+PqKg\n8/I0zgESOhOsoVJhQuqpwOdJNWwJVvY4Joy9AhWzqm7RYkZ1kSdKTJ2AHwwYrfJ/\nxb1yyOsyqqQOoi32kWKfSh29RJaw6VmUQjHSoxxi2MVuIg0wo016zhPdeW2M+hIi\nLC8ZGPxGq8fj6IrBRNblkyuM6bbAggBfZmHCxjQ/1ar8xT1I0zvlYPr+3pwqoRv6\nt535rKGm3w0jTAP9m1sxt3FM8V3FkDFFsFFNHoCRalhT0SI59riabkX1A3jJByh8\no3k8ZEld07CzwQIDAQABo0EwPzAfBgNVHSMEGDAWgBSNLY/dBi6HUGvuyluNgXrg\nfmTVOjAPBgNVHRMBAf8EBTADAQH/MAsGA1UdDwQEAwIC9DANBgkqhkiG9w0BAQsF\nAAOCAQEAZEieJI5mkPhAXqqaF+r1SuZKbjwPN+ZDpb6FTPVXkP06Zu60mptwHKAj\n8m50AVv38jmcvNSz2Pn1CTRcivpqR30O85pQz40sfnhfb9Uaf2WsXrir0KKGXx7a\nHNnkBky2QL0LQc5qHAT384B4D/y8wCqvYnG3MduRmfj29tNnXfYT7abYXNMGxdgp\n3/1VbtQ2IqGYcAkZYUk6yeV0Az3xum2fgP+C5qnKRKsVaVywc35ty8pXrJPT5zpa\n30Ye2jLcQRgP+8pu8SHKFU+uVVS1iyhhRA6C3RVBNY588vt1HjeZZ41tk7jF66+/\nIjfTnIOehqrXz25MYFc3qb6k76jV5A==\n-----END CERTIFICATE----------BEGIN CERTIFICATE-----\nMIIDxjCCAq6gAwIBAgIJAJPTjK4uejYKMA0GCSqGSIb3DQEBCwUAMHAxCzAJBgNV\nBAYTAlVTMQswCQYDVQQIDAJXQTEQMA4GA1UEBwwHU2VhdHRsZTEOMAwGA1UECgwF\nVE1FU0gxDjAMBgNVBAsMBVRNRVNIMSIwIAYJKoZIhvcNAQkBFhNUTUVTSEBub3Jk\nc3Ryb20uY29tMB4XDTE5MDQyOTE3NTQ1MVoXDTIwMDQyODE3NTQ1MVowcDELMAkG\nA1UEBhMCVVMxCzAJBgNVBAgMAldBMRAwDgYDVQQHDAdTZWF0dGxlMQ4wDAYDVQQK\nDAVUTUVTSDEOMAwGA1UECwwFVE1FU0gxIjAgBgkqhkiG9w0BCQEWE1RNRVNIQG5v\ncmRzdHJvbS5jb20wggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQDFSWNP\ngS1CU2UXG8vnjhyrjlLWK8RRdrbcYDmp0jp/jzuh2/PgeTJtDDuD+8Crhylrtv+h\nff6XXHdURvcIXnEBJuN0YCpv0zkwMBsBkwVoSewY/oeZEh5XzsuJDzxS1KTBhT5Y\nzvdkv3t5vWyWP2VIdxsvbggjpWX91+ozuTcsqEu2o5LtgP66y0GmGfjDw53C7e8W\na0NyTCloxcq/MVIKiPlA/OQLbDA2YDdaxTc11wFPqr0fm2jIKjps9LimiSjO5spL\n0tyQNvrg5LR1eXKsYq3f/1+kz1m2dcHnVU1W3wSbzal5tkQoadFaAXZt2XiD1U7v\nkbNjLe6K22WJnft9AgMBAAGjYzBhMB0GA1UdDgQWBBSNLY/dBi6HUGvuyluNgXrg\nfmTVOjAfBgNVHSMEGDAWgBSNLY/dBi6HUGvuyluNgXrgfmTVOjAPBgNVHRMBAf8E\nBTADAQH/MA4GA1UdDwEB/wQEAwIBhjANBgkqhkiG9w0BAQsFAAOCAQEAdlzdzIBf\n/i8iTP6ZOjwYFunUD89kM+I0spZOCoue9MoaodARGXdYa6zsJjqqLy1FieFGYcho\n1PLB2jWF5jCr2PxEaPH71bxB8YQpb5dHSweNNoXVqXwrsBREWRtqk0Ih6ZhzjZZ1\nizXo+nShxiJEQ2o1tJM3BQMBg+tL2wdLQecs4kf25/jPgom+fJH7yhKgwG3OjSXy\n0cp1FtpDUONGksSK2oV7w6iR92ipkzpJZBZEFdN9FGHVTgt8I3hW2PktWlZc8fIJ\n8hpkAhhtj6oHiurc4GGfgGitQGq4eDQeB7C75Kv/P+vN+PhlO9x3+NzSS3BH+LMd\n8oDhIeyirIU3WQ==\n-----END CERTIFICATE-----"
	getCertResponse := acmpca.GetCertificateOutput{
		Certificate:      &endCert,
		CertificateChain: &certChain,
	}

	myClient := mockACMClient{
		IssueCertOutput: &issueCertResponse,
		GetCertOutput:   &getCertResponse,
	}

	subject := ACMPCADelegate{
		acmClient:        myClient,
		CADelegateParams: CADelegateParams{CaARN: "myARN"},
	}

	csr := createCSR()
	_, err := subject.IssueEndEntityCrt(&csr)

	if err != nil {
		t.Error("TestSuccessfulIssueEndCert failed")
	}
}

// TestFailedIssueCert is a unit test that validates correct error propagation when IssueCert fails.
func TestFailedIssueCert(t *testing.T) {
	expectedError := errors.New("issueCertError")
	myClient := mockACMClient{
		IssueCertError: expectedError,
	}

	subject := ACMPCADelegate{
		acmClient:        myClient,
		CADelegateParams: CADelegateParams{CaARN: "myARN"},
	}

	csr := createCSR()
	_, err := subject.IssueEndEntityCrt(&csr)

	if err != expectedError {
		t.Error("TestFailedIssueCert failed")
	}
}

// TestFailedGetCert is a unit test that validates correct error propagation when GetCert fails.
func TestFailedGetCert(t *testing.T) {
	issueCertARN := "myCertificateARN"
	issueCertResponse := acmpca.IssueCertificateOutput{
		CertificateArn: &issueCertARN,
	}

	expectedGetCertError := errors.New("getCertError")

	myClient := mockACMClient{
		IssueCertOutput: &issueCertResponse,
		GetCertError:    expectedGetCertError,
	}

	subject := ACMPCADelegate{
		acmClient:        myClient,
		CADelegateParams: CADelegateParams{CaARN: "myARN"},
	}

	csr := createCSR()
	_, err := subject.IssueEndEntityCrt(&csr)

	if err != expectedGetCertError {
		t.Error("TestFailedGetCert failed")
	}
}

// TestParsingCertificateChainSingle is a unit test that validates that extractTrustChain correctly parses a certificate chain containing a single element.
func TestParsingCertificateChainSingle(t *testing.T) {
	certChain := "-----BEGIN CERTIFICATE-----\nMIID6TCCAtOgAwIBAgIRAKfiZ1mtnUgxuim0oA096JEwCwYJKoZIhvcNAQENMIGM\nMQswCQYDVQQGEwJVUzETMBEGA1UECAwKV2FzaGluZ3RvbjEQMA4GA1UEBwwHU2Vh\ndHRsZTEiMCAGA1UECgwZU09OX09GX1RFU1RfUFJJVkFURV9UTUVTSDEOMAwGA1UE\nCwwFVE1FU0gxIjAgBgNVBAMMGVNPTl9PRl9URVNUX1BSSVZBVEVfVE1FU0gwHhcN\nMTkwNDAzMjAyMDMyWhcNMTkwNTAzMjEyMDMyWjBRMU8wTQYDVQQDE0ZsaW5rZXJk\nLWlkZW50aXR5LmxpbmtlcmQuc2VydmljZWFjY291bnQuaWRlbnRpdHkubGlua2Vy\nZC5jbHVzdGVyLmxvY2FsMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEBm/zJk9C\n8Xl6v7B1pMWz7JS8MyUIz2+oO/+AZ4RFlxLQPVYU0ddo7n0ctcKkVCAluf2cgKPr\nS70GUaQsmPxD8qOCAU0wggFJMFEGA1UdEQRKMEiCRmxpbmtlcmQtaWRlbnRpdHku\nbGlua2VyZC5zZXJ2aWNlYWNjb3VudC5pZGVudGl0eS5saW5rZXJkLmNsdXN0ZXIu\nbG9jYWwwCQYDVR0TBAIwADAfBgNVHSMEGDAWgBSNbCVHbJ+crMCeYY6+1QS0XXjn\nJzAdBgNVHQ4EFgQUhyd2VMGzdDnAfzeSAk6fQntnMUkwDgYDVR0PAQH/BAQDAgWg\nMB0GA1UdJQQWMBQGCCsGAQUFBwMBBggrBgEFBQcDAjB6BgNVHR8EczBxMG+gbaBr\nhmlodHRwOi8vY2FwaW5vLXRlc3QtcHJpdmF0ZS1jYS1jcmwuczMudXMtd2VzdC0y\nLmFtYXpvbmF3cy5jb20vY3JsLzQ2ZThmY2QwLWQ2MTUtNDJhMS05ODk0LTRkYzQ1\nOTQ0ZDU1NC5jcmwwCwYJKoZIhvcNAQENA4IBAQCdgRVHovRfdcPnu0V5EojPKRT6\nzadbJtX1/nQZ476WVDMcTjN+18n4YRZv/yEgYDAH/R9kgAdBmoyDU5meA177GSjP\ndTcilLk+PnZEASYxqnYaWzDi8zhP4S3AhYohkUFVrsTVhXn5CbqcU0ORB9cHKaRM\nJ4zdyi3qYWaVUfShbDHTI9jJtNjDzOMP/FK6JFn1f/xjB5oJIULXmKa873r0Pnnl\nFIXQwSeEQ2TLpEXeD9209aPl0ubciBzcHyHOojRoLPQiCRRVIwoorITAfNKA+EFl\ngc594av/mRJys9cKWyClqKqE6857Aqk/ULsvdtw57IGIEMk34iWNwOqvtXDM\n-----END CERTIFICATE-----\n"

	_, extractError := extractTrustChain(certChain)
	if extractError != nil {
		t.Error("TestParsingCertificateChainSingle failed")
	}
}

// TestParsingCertificateChainMultiple is a unit test that validates that extractTrustChain correctly parses a certificate chain with multiple elements.
// We validate that we can parse a chain that separates the multiple elements with a newline character.
// We validate that we can parse a chain that does not use a newline character to separate multiple elements.
func TestParsingCertificateChainMultiple(t *testing.T) {
	certChainWithNoNewline := "-----BEGIN CERTIFICATE-----\nMIIDujCCAqKgAwIBAgICEAUwDQYJKoZIhvcNAQEFBQAwcDELMAkGA1UEBhMCVVMx\nCzAJBgNVBAgMAldBMRAwDgYDVQQHDAdTZWF0dGxlMQ4wDAYDVQQKDAVUTUVTSDEO\nMAwGA1UECwwFVE1FU0gxIjAgBgkqhkiG9w0BCQEWE1RNRVNIQG5vcmRzdHJvbS5j\nb20wHhcNMTkwMzI5MjE0MDAwWhcNMjAwMzI4MjE0MDAwWjCBjDELMAkGA1UEBhMC\nVVMxEzARBgNVBAgMCldhc2hpbmd0b24xEDAOBgNVBAcMB1NlYXR0bGUxIjAgBgNV\nBAoMGVNPTl9PRl9URVNUX1BSSVZBVEVfVE1FU0gxDjAMBgNVBAsMBVRNRVNIMSIw\nIAYDVQQDDBlTT05fT0ZfVEVTVF9QUklWQVRFX1RNRVNIMIIBIjANBgkqhkiG9w0B\nAQEFAAOCAQ8AMIIBCgKCAQEAxm0KSQwtnboTF3824PzcwPDakkzD9SuXUS4YxXgl\nJ8A6J3FQ/TI/5sbYl9LJsKCb9UEKz4Ao4X2ixWUACe5B9UO1YrWTpKnZ/mlNfTRC\nO6g+EwusTLcRqxepmYQq3Xu0r25EyT3l1vsCXOBI/BlfgRF6lXndwV6Mtqs+t7Yk\nKfFtzadcNc2hz8hm72L1P6d8LGxOTjavI06+tYz2iCm14pld7K5UzjdJVgHD2Aia\n9gL0pzoLSmdDjqKehtYWSx1xw4v6patZaaRxjbqA3zDzuEzsy1xmUHF44wlznOVL\nGseBmYA3DqW1YOGB//asg1ZRa0hEH7FIV8bktj9qTg7SfQIDAQABo0EwPzAfBgNV\nHSMEGDAWgBSYj5Tn7VrJSXj02YbqGnUvypxCGjAPBgNVHRMBAf8EBTADAQH/MAsG\nA1UdDwQEAwIE8DANBgkqhkiG9w0BAQUFAAOCAQEAcrf3OLA+ug+6HkWejZldZPaZ\nIas6MNc6S3FKodRK6miU8MbMfF7PTYfgsP5CiBxCjjg3/0qfXlNcq5zQEOecdWkx\nqAG3y9ZRvTCfLW+T1tU0/5hXcHQzqI3ZmyfTe3dzdTmU+LG6vpcNEwrMed3gQ9Ld\nmwN7OVQPVdTh+0NezxOA5hKzgr4QQ0JErolBPsje5V81l9IfK+6VRZRToC+VQ3YP\nNUy64Z3lRl8ugJTAGfyZZdqkq6Qr7HJKI4rzd71MpDA9x/wwShiIgLnsJfF47v/S\nsZvz5MbgMNeNnKt/PVzf8EIQ9c5x0v/66R+Fu/TX3nXFYjbojqFiA6FHS1rfmQ==\n-----END CERTIFICATE----------BEGIN CERTIFICATE-----\nMIIDxjCCAq6gAwIBAgIJAO6DSG+Jvt0vMA0GCSqGSIb3DQEBDAUAMHAxCzAJBgNV\nBAYTAlVTMQswCQYDVQQIDAJXQTEQMA4GA1UEBwwHU2VhdHRsZTEOMAwGA1UECgwF\nVE1FU0gxDjAMBgNVBAsMBVRNRVNIMSIwIAYJKoZIhvcNAQkBFhNUTUVTSEBub3Jk\nc3Ryb20uY29tMB4XDTE5MDMyODIxMTM0MFoXDTE5MDQyNzIxMTM0MFowcDELMAkG\nA1UEBhMCVVMxCzAJBgNVBAgMAldBMRAwDgYDVQQHDAdTZWF0dGxlMQ4wDAYDVQQK\nDAVUTUVTSDEOMAwGA1UECwwFVE1FU0gxIjAgBgkqhkiG9w0BCQEWE1RNRVNIQG5v\ncmRzdHJvbS5jb20wggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQCeP6ez\n6jFDvmiK54pHhdH9/vwgteQ2SaPCCzH3+LPftE+98r9cYH7q+/AoHHaDUlK3CBRz\n63QrbKFNJfwY5LbEDKma+YR2zSMJLveDlW89hnuwVoCjdfThNqZOoqVOx1QFYBBv\nZ6lvtce2Oc5tmRwOfXudJTragqkMJme0Mn6CCy98R3VGysh7jnPJjb0JD2PygMMx\nKhGuzoM7Ib2Vf6vzOt4oqHFoHkCo1sgLvi7ojCo11ynB0pvequ6HElxgqEnoBUA7\npIhsqe4/gJyC62xjBKON48G/7Ut0xgXMmN0Ir+7nfBiGC8iBVy6smSv+qQ3dAxGx\nUbAUwTKNge9p+1Y3AgMBAAGjYzBhMB0GA1UdDgQWBBSYj5Tn7VrJSXj02YbqGnUv\nypxCGjAfBgNVHSMEGDAWgBSYj5Tn7VrJSXj02YbqGnUvypxCGjAPBgNVHRMBAf8E\nBTADAQH/MA4GA1UdDwEB/wQEAwIBhjANBgkqhkiG9w0BAQwFAAOCAQEAcwQf730e\n6OhPRJ7yU5WVfARck3OgG1kWz4O3F0ZT9SC+85Q920jS3oBfaV2G4cTAsLgvk0rM\n62ghhN7BL/a06+iRkD7xO7w9ftcOReUqlaJ2SQi1L3eL6Cn6pu3mHwWyh3DqYdkW\n2y9SYGTWpmkIW/tG2k+/atnHeC7iEfxlO7Xq1aoAVBGJStR9JFQlETn52nRaG03r\ntMyEU+MrZhJDQR9gIFL/lBC/uLAkkNYqN7E4tbzc4n1rr1OuiTq3XeSdSGrxHkcp\nizHKiX1uPiG0iv5fI43XyTwHHlrfesYhxmFOv/I0HdsuFjQUHyPEq9pB0GsqKsoj\nL/EIQ1Caao5a3g==\n-----END CERTIFICATE-----"
	_, extractNoNewlineError := extractTrustChain(certChainWithNoNewline)
	if extractNoNewlineError != nil {
		t.Errorf("TestParsingCertificateChainMultiple no newline has new line has failed")
	}

	certChainAlreadyHasNewline := "-----BEGIN CERTIFICATE-----\nMIIDujCCAqKgAwIBAgICEAUwDQYJKoZIhvcNAQEFBQAwcDELMAkGA1UEBhMCVVMx\nCzAJBgNVBAgMAldBMRAwDgYDVQQHDAdTZWF0dGxlMQ4wDAYDVQQKDAVUTUVTSDEO\nMAwGA1UECwwFVE1FU0gxIjAgBgkqhkiG9w0BCQEWE1RNRVNIQG5vcmRzdHJvbS5j\nb20wHhcNMTkwMzI5MjE0MDAwWhcNMjAwMzI4MjE0MDAwWjCBjDELMAkGA1UEBhMC\nVVMxEzARBgNVBAgMCldhc2hpbmd0b24xEDAOBgNVBAcMB1NlYXR0bGUxIjAgBgNV\nBAoMGVNPTl9PRl9URVNUX1BSSVZBVEVfVE1FU0gxDjAMBgNVBAsMBVRNRVNIMSIw\nIAYDVQQDDBlTT05fT0ZfVEVTVF9QUklWQVRFX1RNRVNIMIIBIjANBgkqhkiG9w0B\nAQEFAAOCAQ8AMIIBCgKCAQEAxm0KSQwtnboTF3824PzcwPDakkzD9SuXUS4YxXgl\nJ8A6J3FQ/TI/5sbYl9LJsKCb9UEKz4Ao4X2ixWUACe5B9UO1YrWTpKnZ/mlNfTRC\nO6g+EwusTLcRqxepmYQq3Xu0r25EyT3l1vsCXOBI/BlfgRF6lXndwV6Mtqs+t7Yk\nKfFtzadcNc2hz8hm72L1P6d8LGxOTjavI06+tYz2iCm14pld7K5UzjdJVgHD2Aia\n9gL0pzoLSmdDjqKehtYWSx1xw4v6patZaaRxjbqA3zDzuEzsy1xmUHF44wlznOVL\nGseBmYA3DqW1YOGB//asg1ZRa0hEH7FIV8bktj9qTg7SfQIDAQABo0EwPzAfBgNV\nHSMEGDAWgBSYj5Tn7VrJSXj02YbqGnUvypxCGjAPBgNVHRMBAf8EBTADAQH/MAsG\nA1UdDwQEAwIE8DANBgkqhkiG9w0BAQUFAAOCAQEAcrf3OLA+ug+6HkWejZldZPaZ\nIas6MNc6S3FKodRK6miU8MbMfF7PTYfgsP5CiBxCjjg3/0qfXlNcq5zQEOecdWkx\nqAG3y9ZRvTCfLW+T1tU0/5hXcHQzqI3ZmyfTe3dzdTmU+LG6vpcNEwrMed3gQ9Ld\nmwN7OVQPVdTh+0NezxOA5hKzgr4QQ0JErolBPsje5V81l9IfK+6VRZRToC+VQ3YP\nNUy64Z3lRl8ugJTAGfyZZdqkq6Qr7HJKI4rzd71MpDA9x/wwShiIgLnsJfF47v/S\nsZvz5MbgMNeNnKt/PVzf8EIQ9c5x0v/66R+Fu/TX3nXFYjbojqFiA6FHS1rfmQ==\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\nMIIDxjCCAq6gAwIBAgIJAO6DSG+Jvt0vMA0GCSqGSIb3DQEBDAUAMHAxCzAJBgNV\nBAYTAlVTMQswCQYDVQQIDAJXQTEQMA4GA1UEBwwHU2VhdHRsZTEOMAwGA1UECgwF\nVE1FU0gxDjAMBgNVBAsMBVRNRVNIMSIwIAYJKoZIhvcNAQkBFhNUTUVTSEBub3Jk\nc3Ryb20uY29tMB4XDTE5MDMyODIxMTM0MFoXDTE5MDQyNzIxMTM0MFowcDELMAkG\nA1UEBhMCVVMxCzAJBgNVBAgMAldBMRAwDgYDVQQHDAdTZWF0dGxlMQ4wDAYDVQQK\nDAVUTUVTSDEOMAwGA1UECwwFVE1FU0gxIjAgBgkqhkiG9w0BCQEWE1RNRVNIQG5v\ncmRzdHJvbS5jb20wggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQCeP6ez\n6jFDvmiK54pHhdH9/vwgteQ2SaPCCzH3+LPftE+98r9cYH7q+/AoHHaDUlK3CBRz\n63QrbKFNJfwY5LbEDKma+YR2zSMJLveDlW89hnuwVoCjdfThNqZOoqVOx1QFYBBv\nZ6lvtce2Oc5tmRwOfXudJTragqkMJme0Mn6CCy98R3VGysh7jnPJjb0JD2PygMMx\nKhGuzoM7Ib2Vf6vzOt4oqHFoHkCo1sgLvi7ojCo11ynB0pvequ6HElxgqEnoBUA7\npIhsqe4/gJyC62xjBKON48G/7Ut0xgXMmN0Ir+7nfBiGC8iBVy6smSv+qQ3dAxGx\nUbAUwTKNge9p+1Y3AgMBAAGjYzBhMB0GA1UdDgQWBBSYj5Tn7VrJSXj02YbqGnUv\nypxCGjAfBgNVHSMEGDAWgBSYj5Tn7VrJSXj02YbqGnUvypxCGjAPBgNVHRMBAf8E\nBTADAQH/MA4GA1UdDwEB/wQEAwIBhjANBgkqhkiG9w0BAQwFAAOCAQEAcwQf730e\n6OhPRJ7yU5WVfARck3OgG1kWz4O3F0ZT9SC+85Q920jS3oBfaV2G4cTAsLgvk0rM\n62ghhN7BL/a06+iRkD7xO7w9ftcOReUqlaJ2SQi1L3eL6Cn6pu3mHwWyh3DqYdkW\n2y9SYGTWpmkIW/tG2k+/atnHeC7iEfxlO7Xq1aoAVBGJStR9JFQlETn52nRaG03r\ntMyEU+MrZhJDQR9gIFL/lBC/uLAkkNYqN7E4tbzc4n1rr1OuiTq3XeSdSGrxHkcp\nizHKiX1uPiG0iv5fI43XyTwHHlrfesYhxmFOv/I0HdsuFjQUHyPEq9pB0GsqKsoj\nL/EIQ1Caao5a3g==\n-----END CERTIFICATE-----"
	_, extractAlreadyHasNewlineError := extractTrustChain(certChainAlreadyHasNewline)
	if extractAlreadyHasNewlineError != nil {
		t.Errorf("TestParsingCertificateChainMultiple has newline has failed")
	}
}
