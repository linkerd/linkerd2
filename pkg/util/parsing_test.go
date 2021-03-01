package util

import (
	"fmt"
	"reflect"
	"testing"
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
			ports, err := ParsePorts(tc.ports)
			if err != nil {
				t.Fatalf("could not parse ports: %v", err)
			}

			if !reflect.DeepEqual(ports, tc.result) {
				t.Fatalf("Expected output: \"%v\", got: \"%v\"", tc.result, ports)
			}
		})
	}
}
