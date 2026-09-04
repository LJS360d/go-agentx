// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import (
	"strings"
	"testing"
	"time"

	"github.com/LJS360d/go-agentx/value"
)

// Every String method is reachable from a log line on a request path, so each
// has to cope with a zero value and with codes it does not know - a formatter
// that panics turns a log statement into a crash.
func TestStringersHandleZeroAndUnknownValues(t *testing.T) {
	var variables Variables
	variables.Add(value.OID{1, 3, 6, 1}, VariableTypeInteger, int32(1))

	stringers := []struct {
		name string
		in   interface{ String() string }
	}{
		{"Header", &Header{}},
		{"HeaderPacket", &HeaderPacket{Header: &Header{}, Packet: &Response{}}},
		{"Response", &Response{}},
		{"Notify", &Notify{}},
		{"Variable", &Variable{}},
		{"Variable with a value", &variables[0]},
		{"Variables", variables},
		{"Range", Range{}},
		{"Range with include", Range{From: ObjectIdentifier{Include: 1}, To: ObjectIdentifier{Include: 1}}},
		{"ObjectIdentifier", oid(1, 3, 6, 1)},
		{"Timeout", Timeout{Duration: time.Second}},
		{"Close", &Close{}},
		{"Type", TypeGet},
		{"unknown Type", Type(200)},
		{"Flags none", Flags(0)},
		{"Flags all", Flags(0xFF)},
		{"Error", ErrorNone},
		{"unknown Error", Error(9999)},
		{"Reason", ReasonShutdown},
		{"unknown Reason", Reason(99)},
		{"VariableType", VariableTypeCounter64},
		{"unknown VariableType", VariableType(9999)},
	}

	for _, s := range stringers {
		t.Run(s.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("String panicked: %v", r)
				}
			}()
			if s.in.String() == "" {
				t.Fatal("String returned an empty string")
			}
		})
	}
}

// Every PDU type constant must have a name, and every unknown one must say so
// rather than being reported as some other type.
func TestTypeStringsAreDistinct(t *testing.T) {
	seen := make(map[string]Type)
	for t2 := TypeOpen; t2 <= TypeResponse; t2++ {
		name := t2.String()
		// The fallback is the exact string "TypeUnknown"; a real name that
		// merely contains "Unknown" (there is none today, but there could be)
		// must not be mistaken for it.
		if name == "TypeUnknown" {
			t.Fatalf("type %d has no name", t2)
		}
		if previous, ok := seen[name]; ok {
			t.Fatalf("types %d and %d share the name %s", previous, t2, name)
		}
		seen[name] = t2
	}
	if got := Type(0).String(); got != "TypeUnknown" {
		t.Fatalf("Type(0) = %s, want an unknown-type name", got)
	}
}

// Error codes are what a caller matches on; a code with no name is a code
// nobody can diagnose from a log.
func TestErrorStringsCoverEveryCode(t *testing.T) {
	codes := []Error{
		ErrorNone, ErrorGenErr, ErrorNoAccess, ErrorWrongType, ErrorWrongLength,
		ErrorWrongEncoding, ErrorWrongValue, ErrorNoCreation, ErrorInconsistentValue,
		ErrorResourceUnavailable, ErrorCommitFailed, ErrorUndoFailed, ErrorNotWritable,
		ErrorInconsistentName, ErrorOpenFailed, ErrorNotOpen, ErrorIndexWrongType,
		ErrorIndexAlreadyAllocated, ErrorIndexNoneAvailable, ErrorIndexNotAllocated,
		ErrorUnsupportedContext, ErrorDuplicateRegistration, ErrorUnknownRegistration,
		ErrorUnknownAgentCaps, ErrorParse, ErrorRequestDenied, ErrorProcessing,
	}

	for _, code := range codes {
		// The fallback is "ErrorUnknown (n)". Matching on "Unknown" alone
		// would also flag ErrorUnknownRegistration and ErrorUnknownAgentCaps,
		// which are real names.
		if strings.HasPrefix(code.String(), "ErrorUnknown (") {
			t.Errorf("error %d has no name", code)
		}
		// Error implements the error interface so a Handler can return a code
		// directly and Session.handle can recover it with errors.As.
		if code.Error() != code.String() {
			t.Errorf("Error() and String() disagree for %d", code)
		}
	}
}

// Index allocation is not implemented; the decoders must say so instead of
// quietly reporting success on a PDU they ignored.
func TestIndexPDUsReportThatDecodingIsNotImplemented(t *testing.T) {
	if err := (&AllocateIndex{}).UnmarshalBinary(nil); err == nil {
		t.Fatal("AllocateIndex.UnmarshalBinary reported success")
	}
	if err := (&DeallocateIndex{}).UnmarshalBinary(nil); err == nil {
		t.Fatal("DeallocateIndex.UnmarshalBinary reported success")
	}
}

// The unknown-value fallbacks have to include the value itself, or a log line
// about an unexpected code says nothing about which code it was.
func TestUnknownValueStringsIncludeTheValue(t *testing.T) {
	cases := map[string]string{
		Error(9999).String():        "9999",
		Reason(99).String():         "99",
		VariableType(9999).String(): "9999",
	}
	for got, want := range cases {
		if !strings.Contains(got, want) {
			t.Errorf("%q does not mention the value %s", got, want)
		}
	}
	// A null object identifier has no sub-identifiers, so its textual form is
	// legitimately empty (RFC 2741 5.1).
	if got := (ObjectIdentifier{}).String(); got != "" {
		t.Errorf("null ObjectIdentifier = %q, want the empty string", got)
	}
}
