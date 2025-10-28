// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

// TestSet defines the pdu test set packet.
type TestSet struct {
	Variables Variables
}

// Type returns the pdu packet type.
func (t *TestSet) Type() Type {
	return TypeTestSet
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (t *TestSet) MarshalBinary() ([]byte, error) {
	return t.Variables.MarshalBinary()
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
func (t *TestSet) UnmarshalBinary(data []byte) error {
	return t.Variables.UnmarshalBinary(data)
}
