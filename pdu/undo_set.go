// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

// UndoSet defines the pdu undo set packet.
type UndoSet struct {
	Variables Variables
}

// Type returns the pdu packet type.
func (u *UndoSet) Type() Type {
	return TypeUndoSet
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (u *UndoSet) MarshalBinary() ([]byte, error) {
	return u.Variables.MarshalBinary()
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
func (u *UndoSet) UnmarshalBinary(data []byte) error {
	return u.Variables.UnmarshalBinary(data)
}
