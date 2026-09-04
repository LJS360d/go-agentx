// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import "fmt"

// UndoSet defines the pdu undo set packet.
//
// RFC 2741 6.2.9: "These PDUs consist of the AgentX header only." The bindings
// the operation applies to are the ones carried by the preceding
// agentx-TestSet-PDU of the same transaction.
type UndoSet struct {
	// Variables is always empty.
	//
	// Deprecated: an agentx-UndoSet-PDU has no payload. The field is kept so
	// that existing code compiles; it is neither encoded nor decoded.
	Variables Variables
}

// Type returns the pdu packet type.
func (u *UndoSet) Type() Type {
	return TypeUndoSet
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (u *UndoSet) MarshalBinary() ([]byte, error) {
	return []byte{}, nil
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
func (u *UndoSet) UnmarshalBinary(data []byte) error {
	if len(data) > 0 {
		return fmt.Errorf("undo set: expected an empty payload, got %d bytes", len(data))
	}
	return nil
}
