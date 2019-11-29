package cmd

import (
	"strings"
	"testing"
)

func TestMapRangeSlice(t *testing.T) {
	assertRangesEqual(t, []string{"23"}, []uint{23})
	assertRangesEqual(t, []string{"23-23"}, []uint{23})
	assertRangesEqual(t, []string{"23", "25"}, []uint{23, 25})
	assertRangesEqual(t, []string{"25-27"}, []uint{25, 26, 27})
	assertRangesEqual(t, []string{"20-21", "25-27"}, []uint{20, 21, 25, 26, 27})
	// Note that the slice is NOT sorted!
	assertRangesEqual(t, []string{"25-27", "17"}, []uint{25, 26, 27, 17})
}

func TestMapRangeSlice_NilSlice(t *testing.T) {
	var check []uint
	err := MapRangeSlice(&check, nil)
	if err != nil {
		t.Fatal("expected no error; got", err)
	}
	if len(check) != 0 {
		t.Fatal("expected empty slice")
	}
}

func TestMapRangeSlice_EmptySlice(t *testing.T) {
	var check []uint
	err := MapRangeSlice(&check, []string{})
	if err != nil {
		t.Fatal("expected no error; got", err)
	}
	if len(check) != 0 {
		t.Fatal("expected empty slice")
	}
}

func TestMapRangeSlice_NonNumeric(t *testing.T) {
	var check []uint
	err := MapRangeSlice(&check, []string{"not a number"})
	if err == nil {
		t.Fatal("expecting error")
	}
}

func TestMapRangeSlice_MissingLowerBound(t *testing.T) {
	var check []uint
	err := MapRangeSlice(&check, []string{"-27"})
	if err == nil {
		t.Fatal("expecting error")
	}
	if !strings.Contains(err.Error(), "not a valid lower-bound") {
		t.Fatalf("unexpected error encountered: \"%s\"", err.Error())
	}
}

func TestMapRangeSlice_MissingUpperBound(t *testing.T) {
	var check []uint
	err := MapRangeSlice(&check, []string{"27-"})
	if err == nil {
		t.Fatal("expecting error")
	}
	if !strings.Contains(err.Error(), "not a valid upper-bound") {
		t.Fatalf("unexpected error encountered: \"%s\"", err.Error())
	}
}

func TestMapRangeSlice_DescendingRange(t *testing.T) {
	var check []uint
	err := MapRangeSlice(&check, []string{"29-27"})
	if err == nil {
		t.Fatal("expecting error")
	}
	if !strings.Contains(err.Error(), "upper-bound must be greater") {
		t.Fatalf("unexpected error encountered: \"%s\"", err.Error())
	}
}

func TestMapRangeSlice_NegativePortRange(t *testing.T) {
	var check []uint
	err := MapRangeSlice(&check, []string{"-29-27"})
	if err == nil {
		t.Fatal("expecting error")
	}
	if !strings.Contains(err.Error(), "ranges expected as <lower>-<upper>") {
		t.Fatalf("unexpected error encountered: \"%s\"", err.Error())
	}
}

func assertRangesEqual(t *testing.T, test []string, expected []uint) {
	var check []uint
	// Convert the 'test' string slice
	if err := MapRangeSlice(&check, test); err != nil {
		t.Fatal("expected no error; got", err)
	}
	// Ensure the expected results match
	if check != nil && expected != nil {
		if len(check) != len(expected) {
			t.Fatal("compared slices differ in size")
		}
		for i, value := range check {
			if value != expected[i] {
				t.Fatalf("mismatch: got \"%d\" expected \"%d\"", value, expected[i])
			}
		}
	}
}
