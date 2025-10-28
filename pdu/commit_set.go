// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

// CommitSet defines the pdu commit set packet.
type CommitSet struct {
	Variables Variables
}

// Type returns the pdu packet type.
func (c *CommitSet) Type() Type {
	return TypeCommitSet
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (c *CommitSet) MarshalBinary() ([]byte, error) {
	return c.Variables.MarshalBinary()
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
func (c *CommitSet) UnmarshalBinary(data []byte) error {
	return c.Variables.UnmarshalBinary(data)
}

