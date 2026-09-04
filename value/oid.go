// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package value

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// OID defines an OID.
type OID []uint32

// ParseOID parses the provided string and returns a valid oid. If one of the
// subidentifers canot be parsed to an uint32, the function will panic.
func ParseOID(text string) (OID, error) {
	var result OID

	parts := strings.Split(text, ".")
	for _, part := range parts {
		subidentifier, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("parse uint [%s]: %w", part, err)
		}
		result = append(result, uint32(subidentifier))
	}

	return result, nil
}

// MustParseOID works like ParseOID expect it panics on a parsing error.
func MustParseOID(text string) OID {
	result, err := ParseOID(text)
	if err != nil {
		panic(err)
	}
	return result
}

// First returns the first n subidentifiers as a new oid. Asking for more
// subidentifiers than the oid has returns the whole oid.
func (o OID) First(count int) OID {
	if count < 0 {
		return nil
	}
	if count > len(o) {
		count = len(o)
	}
	return o[:count]
}

// CommonPrefix compares the oid with the provided one and
// returns a new oid containing all matching prefix subidentifiers.
func (o OID) CommonPrefix(other OID) OID {
	matchCount := 0

	for index, subidentifier := range o {
		if index >= len(other) || subidentifier != other[index] {
			break
		}
		matchCount++
	}

	return o[:matchCount]
}

// CompareOIDs returns an integer comparing two OIDs lexicographically.
// The result will be 0 if oid1 == oid2, -1 if oid1 < oid2, +1 if oid1 > oid2.
//
// A nil OID is treated as the empty OID, which sorts before every other value
// and equals itself. Callers that need "no upper bound" semantics for a null
// ending OID (RFC 2741 5.2) must special-case it before comparing; an empty
// OID compares as the smallest value, not the largest.
func CompareOIDs(oid1, oid2 OID) int {
	for i := 0; i < len(oid1) && i < len(oid2); i++ {
		if oid1[i] < oid2[i] {
			return -1
		}
		if oid1[i] > oid2[i] {
			return 1
		}
	}

	switch {
	case len(oid1) < len(oid2):
		return -1
	case len(oid1) > len(oid2):
		return 1
	}
	return 0
}

// SortOIDs performs sorting of the OID list.
func SortOIDs(oids []OID) {
	sort.Slice(oids, func(i, j int) bool {
		return CompareOIDs(oids[i], oids[j]) == -1
	})
}

func (o OID) String() string {
	var parts []string

	for _, subidentifier := range o {
		parts = append(parts, fmt.Sprintf("%d", subidentifier))
	}

	return strings.Join(parts, ".")
}
