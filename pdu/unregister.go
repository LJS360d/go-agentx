// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import "fmt"

// Unregister defines the pdu unregister packet.
type Unregister struct {
	Timeout Timeout
	Subtree ObjectIdentifier
}

// Type returns the pdu packet type.
func (u *Unregister) Type() Type {
	return TypeUnregister
}

// MarshalBinary returns the pdu packet as a slice of bytes.
//
// RFC 2741 6.2.4: unlike the Register PDU, the first payload byte of an
// Unregister is <reserved> and must be zero-filled - there is no u.timeout
// field. Only the priority is carried over from Timeout, and it must match the
// priority the region was registered with.
func (u *Unregister) MarshalBinary() ([]byte, error) {
	result := []byte{0x00, u.Timeout.Priority, 0x00, 0x00}

	subtreeBytes, err := u.Subtree.MarshalBinary()
	if err != nil {
		return nil, err
	}

	return append(result, subtreeBytes...), nil
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
//
// A subagent never receives an agentx-Unregister-PDU; this exists so the type
// satisfies Packet and so tests can round-trip what the library encodes.
func (u *Unregister) UnmarshalBinary(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("unregister: short buffer: got %d bytes, want at least 4", len(data))
	}
	u.Timeout.Duration = 0
	u.Timeout.Priority = data[1]
	return u.Subtree.UnmarshalBinary(data[4:])
}
