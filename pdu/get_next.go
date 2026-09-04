// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import "encoding/binary"

// GetNext defines the pdu get next packet.
type GetNext struct {
	SearchRanges Ranges
}

// Type returns the pdu packet type.
func (g *GetNext) Type() Type {
	return TypeGetNext
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (g *GetNext) MarshalBinary() ([]byte, error) {
	return g.SearchRanges.MarshalBinary()
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
func (g *GetNext) UnmarshalBinary(data []byte) error {
	return g.SearchRanges.UnmarshalBinary(data)
}

// UnmarshalBinaryOrder sets the packet structure from the provided slice of
// bytes, decoding multi-byte fields in the byte order the header declared.
func (g *GetNext) UnmarshalBinaryOrder(data []byte, order binary.ByteOrder) error {
	return g.SearchRanges.UnmarshalBinaryOrder(data, order)
}
