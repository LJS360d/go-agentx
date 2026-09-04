// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import "testing"

// RFC 2741 7.2.4.1: a request this implementation cannot process should be
// answered with an error, not crash the receiver. Unsupported is what the
// client falls back to for a PDU type it has no concrete struct for (e.g.
// GetBulk); it must decode any payload without error so session handling can
// reach the point of sending genErr back.
func TestUnsupportedRoundTrip(t *testing.T) {
	u := &Unsupported{PacketType: TypeGetBulk}

	if u.Type() != TypeGetBulk {
		t.Fatalf("Type() = %v, want %v", u.Type(), TypeGetBulk)
	}

	data, err := u.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	if err := u.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary of own output: %v", err)
	}

	// Arbitrary, non-empty payload must not error either - real GetBulk PDUs
	// carry NonRepeaters/MaxRepetitions/SearchRanges this type ignores.
	if err := u.UnmarshalBinary([]byte{1, 2, 3, 4, 5, 6, 7, 8}); err != nil {
		t.Fatalf("UnmarshalBinary of arbitrary payload: %v", err)
	}
}
