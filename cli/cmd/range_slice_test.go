package cmd

import (
	"strings"
	"testing"
)

func TestValidateRangeSlice(t *testing.T) {
	tests := [][]string{
		nil,
		{},
		{"0"},
		{"23"},
		{"23-23"},
		{"25-27"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt, ","), func(t *testing.T) { //scopelint:ignore
			assertNoError(t, validateRangeSlice(tt))
		})
	}
}

func TestValidateRangeSlice_Errors(t *testing.T) {
	tests := []struct {
		input  []string
		expect string
	}{
		{[]string{""}, "not a valid port"},
		{[]string{"notanumber"}, "not a valid port"},
		{[]string{"not-number"}, "not a valid lower-bound"},
		{[]string{"-23-25"}, "ranges expected as"},
		{[]string{"-23"}, "not a valid lower-bound"},
		{[]string{"23-"}, "not a valid upper-bound"},
		{[]string{"25-23"}, "upper-bound must be greater than or equal to"},
		{[]string{"65536"}, "not a valid port"},
		{[]string{"10-65536"}, "not a valid upper-bound"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.input, ","), func(t *testing.T) { //scopelint:ignore
			assertError(t, validateRangeSlice(tt.input), tt.expect)
		})
	}
}

func assertNoError(t *testing.T, err error) {
	if err != nil {
		t.Fatalf("expected no error; got %s", err)
	}
}

// assertError confirms that the provided is an error having the provided message.
func assertError(t *testing.T, err error, containing string) {
	if err == nil {
		t.Fatalf("expected error containing '%s' but got nothing", containing)
	}
	if !strings.Contains(err.Error(), containing) {
		t.Fatalf("expected error to contain '%s' but received '%s'", containing, err.Error())
	}
}
