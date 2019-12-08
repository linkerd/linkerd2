package cmd

import (
	"strings"
	"testing"
)

func TestValidateRangeSlice(t *testing.T) {
	assertNoError(t, validateRangeSlice(nil))
	assertNoError(t, validateRangeSlice([]string{}))
	assertNoError(t, validateRangeSlice([]string{"23"}))
	assertNoError(t, validateRangeSlice([]string{"23-23"}))
	assertNoError(t, validateRangeSlice([]string{"25-27"}))
	assertNoError(t, validateRangeSlice([]string{"23"}))
	assertNoError(t, validateRangeSlice([]string{"23"}))

	assertError(t, validateRangeSlice([]string{""}), "not a valid port")
	assertError(t, validateRangeSlice([]string{"notanumber"}), "not a valid port")
	assertError(t, validateRangeSlice([]string{"not-number"}), "not a valid lower-bound")
	assertError(t, validateRangeSlice([]string{"-23-25"}), "ranges expected as")
	assertError(t, validateRangeSlice([]string{"-23"}), "not a valid lower-bound")
	assertError(t, validateRangeSlice([]string{"25-23"}), "upper-bound must be greater than or equal to")
}

func assertNoError(t *testing.T, err error) {
	if err != nil {
		t.Fatal("expected no error; got", err)
	}
}

// assertError confirms that the provided is an error having the provided message.
func assertError(t *testing.T, err error, containing string) {
	if err == nil {
		t.Fatal("expected error; got nothing")
	}
	if !strings.Contains(err.Error(), containing) {
		t.Fatalf("expected error to contain '%s' but received '%s'", containing, err.Error())
	}
}
