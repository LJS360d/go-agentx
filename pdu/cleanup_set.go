// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

// CleanupSet defines the pdu cleanup set packet.
type CleanupSet struct {
	Variables Variables
}

// Type returns the pdu packet type.
func (c *CleanupSet) Type() Type {
	return TypeCleanupSet
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (c *CleanupSet) MarshalBinary() ([]byte, error) {
	return c.Variables.MarshalBinary()
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
func (c *CleanupSet) UnmarshalBinary(data []byte) error {
	return c.Variables.UnmarshalBinary(data)
}
