// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import "testing"

// RFC 2741 6.1: the header's four multi-byte fields (session ID, transaction
// ID, packet ID, payload length) are encoded in the byte order the sender
// declares via the network-byte-order flag - little-endian when unset,
// big-endian ("network byte order") when set. A decoder that ignores the
// flag misreads every field of a packet from a peer that sets it.
func TestHeaderRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		flags Flags
	}{
		{"little-endian (flag unset)", 0},
		{"big-endian (network byte order flag set)", FlagNetworkByteOrder},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := &Header{
				Version:       1,
				Type:          TypeResponse,
				Flags:         c.flags,
				SessionID:     0x01020304,
				TransactionID: 0x05060708,
				PacketID:      0x090a0b0c,
				PayloadLength: 0x000d0f10,
			}

			data, err := want.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary: %v", err)
			}
			if len(data) != HeaderSize {
				t.Fatalf("MarshalBinary: got %d bytes, want %d", len(data), HeaderSize)
			}

			got := &Header{}
			if err := got.UnmarshalBinary(data); err != nil {
				t.Fatalf("UnmarshalBinary: %v", err)
			}

			if *got != *want {
				t.Fatalf("round trip mismatch: got %+v, want %+v", got, want)
			}
		})
	}
}

// A header the wire declares as network-byte-order must decode as
// big-endian even when this host is little-endian - the flag governs the
// packet's encoding, not the local platform's.
func TestHeaderUnmarshalHonorsNetworkByteOrderFlag(t *testing.T) {
	data := []byte{
		1,                          // version
		byte(TypeGet),              // type
		byte(FlagNetworkByteOrder), // flags
		0x00,                       // reserved
		0x00, 0x00, 0x00, 0x2a,     // session ID = 42, big-endian
		0x00, 0x00, 0x00, 0x00, // transaction ID = 0
		0x00, 0x00, 0x00, 0x00, // packet ID = 0
		0x00, 0x00, 0x00, 0x00, // payload length = 0
	}

	h := &Header{}
	if err := h.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	if h.SessionID != 42 {
		t.Fatalf("SessionID = %d, want 42 (a little-endian decode would give %d)", h.SessionID, uint32(0x2a000000))
	}
}

func TestHeaderUnmarshalShortBuffer(t *testing.T) {
	h := &Header{}
	if err := h.UnmarshalBinary(make([]byte, HeaderSize-1)); err == nil {
		t.Fatal("UnmarshalBinary: want error for a buffer shorter than HeaderSize, got nil")
	}
}
