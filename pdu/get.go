// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import (
	"encoding/binary"

	"github.com/LJS360d/go-agentx/value"
)

// Get defines the pdu get packet.
//
// RFC 2741 6.2.5: the payload is a SearchRangeList, one entry per requested
// variable, and RFC 2741 7.2.3 requires a conformant subagent to support
// multiple variables in a single PDU. Within a Get the ending OID of every
// SearchRange is a null Object Identifier.
type Get struct {
	SearchRanges Ranges
}

// GetOID returns the oid of the first search range.
//
// Deprecated: a Get may carry any number of search ranges; use SearchRanges to
// see all of them. This accessor only reports the first.
func (g *Get) GetOID() value.OID {
	if len(g.SearchRanges) == 0 {
		return nil
	}
	return g.SearchRanges[0].From.GetIdentifier()
}

// SetOID sets the provided oid as the only search range.
func (g *Get) SetOID(oid value.OID) {
	if len(g.SearchRanges) == 0 {
		g.SearchRanges = Ranges{Range{}}
	}
	g.SearchRanges[0].From.SetIdentifier(oid)
}

// Type returns the pdu packet type.
func (g *Get) Type() Type {
	return TypeGet
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (g *Get) MarshalBinary() ([]byte, error) {
	return g.SearchRanges.MarshalBinary()
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
func (g *Get) UnmarshalBinary(data []byte) error {
	return g.SearchRanges.UnmarshalBinary(data)
}

// UnmarshalBinaryOrder sets the packet structure from the provided slice of
// bytes, decoding multi-byte fields in the byte order the header declared.
func (g *Get) UnmarshalBinaryOrder(data []byte, order binary.ByteOrder) error {
	return g.SearchRanges.UnmarshalBinaryOrder(data, order)
}
