package tls

import (
	"testing"
	"time"
)

func getCa(validFrom time.Time, issuerCertLifetime time.Duration, endCertLifetime time.Duration) (*CA, error) {
	key, err := GenerateKey()
	if err != nil {
		return nil, err
	}

	ca, err := CreateRootCA("fake-name", key, Validity{ValidFrom: &validFrom, Lifetime: issuerCertLifetime})
	if err != nil {
		return nil, err
	}

	return NewCA(ca.Cred, Validity{ValidFrom: &validFrom, Lifetime: endCertLifetime}), nil
}

func TestCaIssuesCertsWithCorrectExpiration(t *testing.T) {

	validFrom := time.Now().UTC().Round(time.Second)

	testCases := []struct {
		desc                   string
		validFrom              time.Time
		issuerLifeTime         time.Duration
		endCertLifetime        time.Duration
		expectedCertExpiration time.Time
	}{
		{
			desc:                   "issuer cert expires after end cert",
			validFrom:              validFrom,
			issuerLifeTime:         time.Hour * 48,
			endCertLifetime:        time.Hour * 24,
			expectedCertExpiration: validFrom.Add(time.Hour * 24).Add(DefaultClockSkewAllowance),
		},
		{
			desc:                   "issuer cert expires before end cert",
			validFrom:              validFrom,
			issuerLifeTime:         time.Hour * 10,
			endCertLifetime:        time.Hour * 24,
			expectedCertExpiration: validFrom.Add(time.Hour * 10).Add(DefaultClockSkewAllowance),
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {

			ca, err := getCa(tc.validFrom, tc.issuerLifeTime, tc.endCertLifetime)
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}
			crt, err := ca.GenerateEndEntityCred("fake-name")
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}
			if crt.Certificate.NotAfter != tc.expectedCertExpiration {
				t.Fatalf("Expected cert expiration %v but got %v", tc.expectedCertExpiration, crt.Certificate.NotAfter)
			}
		})
	}

}
