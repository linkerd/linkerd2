package util

import (
	"fmt"
	"testing"

	"github.com/go-test/deep"
)

func TestParsePorts(t *testing.T) {
	testCases := []struct {
		ports  string
		result map[uint32]struct{}
	}{
		{
			"25,443,587,3306,5432,11211",
			map[uint32]struct{}{
				25:    {},
				443:   {},
				587:   {},
				3306:  {},
				5432:  {},
				11211: {},
			},
		},
		{
			"25,443-447,3306,5432-5435,11211",
			map[uint32]struct{}{
				25:    {},
				443:   {},
				444:   {},
				445:   {},
				446:   {},
				447:   {},
				3306:  {},
				5432:  {},
				5433:  {},
				5434:  {},
				5435:  {},
				11211: {},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("test %s", tc.ports), func(t *testing.T) {
			ports := ParsePorts(tc.ports)
			if diff := deep.Equal(ports, tc.result); diff != nil {
				t.Errorf("%v", diff)
			}
		})
	}
}
