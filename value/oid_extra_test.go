// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package value_test

import (
	"testing"

	"github.com/LJS360d/go-agentx/value"
)

func TestParseOID(t *testing.T) {
	cases := []struct {
		text string
		want string
		ok   bool
	}{
		{"1.3.6.1", "1.3.6.1", true},
		{"1", "1", true},
		{"4294967295", "4294967295", true},
		{"", "", false},
		{".1.3.6.1", "", false},
		{"1.3.6.1.", "", false},
		{"1..3", "", false},
		{"1.3.-6", "", false},
		{"4294967296", "", false}, // does not fit in a sub-identifier
		{"1.3.six", "", false},
	}

	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			got, err := value.ParseOID(c.text)
			if c.ok {
				if err != nil {
					t.Fatalf("ParseOID(%q): %v", c.text, err)
				}
				if got.String() != c.want {
					t.Fatalf("ParseOID(%q) = %s, want %s", c.text, got, c.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("ParseOID(%q) = %v, want an error", c.text, got)
			}
		})
	}
}

func TestMustParseOIDPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustParseOID did not panic on an invalid OID")
		}
	}()
	value.MustParseOID("nope")
}

// CompareOIDs is what orders a getNext walk, so its treatment of prefixes and
// of the empty OID has to be exact. The empty OID is the smallest value and
// equals itself; callers wanting "unbounded" semantics for a null ending OID
// (RFC 2741 5.2) special-case it instead of relying on the comparison.
func TestCompareOIDsOrdering(t *testing.T) {
	cases := []struct {
		a, b value.OID
		want int
	}{
		{nil, nil, 0},
		{value.OID{}, nil, 0},
		{nil, value.OID{1}, -1},
		{value.OID{1}, nil, 1},
		{value.OID{1, 3, 6}, value.OID{1, 3, 6}, 0},
		{value.OID{1, 3, 6}, value.OID{1, 3, 6, 1}, -1},
		{value.OID{1, 3, 6, 1}, value.OID{1, 3, 6}, 1},
		{value.OID{1, 3, 2}, value.OID{1, 3, 10}, -1},
		{value.OID{1, 3, 10}, value.OID{1, 3, 2}, 1},
		{value.OID{0}, value.OID{4294967295}, -1},
	}

	for _, c := range cases {
		if got := value.CompareOIDs(c.a, c.b); got != c.want {
			t.Errorf("CompareOIDs(%v, %v) = %d, want %d", c.a, c.b, got, c.want)
		}
		// Antisymmetry: reversing the arguments must reverse the sign.
		if got := value.CompareOIDs(c.b, c.a); got != -c.want {
			t.Errorf("CompareOIDs(%v, %v) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}

func TestFirstDoesNotPanic(t *testing.T) {
	oid := value.OID{1, 3, 6, 1}

	if got := oid.First(2).String(); got != "1.3" {
		t.Fatalf("First(2) = %s, want 1.3", got)
	}
	if got := oid.First(99).String(); got != "1.3.6.1" {
		t.Fatalf("First(99) = %s, want the whole OID", got)
	}
	if got := oid.First(-1); got != nil {
		t.Fatalf("First(-1) = %v, want nil", got)
	}
}

func TestCommonPrefixEdges(t *testing.T) {
	cases := []struct {
		a, b value.OID
		want string
	}{
		{value.OID{1, 3, 6, 1}, value.OID{1, 3, 6, 2}, "1.3.6"},
		{value.OID{1, 3, 6, 1}, value.OID{2}, ""},
		{value.OID{1, 3}, value.OID{1, 3, 6}, "1.3"},
		{value.OID{1, 3}, nil, ""},
		{nil, value.OID{1, 3}, ""},
	}

	for _, c := range cases {
		if got := c.a.CommonPrefix(c.b).String(); got != c.want {
			t.Errorf("%v.CommonPrefix(%v) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}
