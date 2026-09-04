// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import "fmt"

// Register defines the pdu register packet.
//
// Registration of an OID range (RFC 2741 6.2.3 r.range_subid / r.upper_bound)
// is not supported: r.range_subid is always encoded as 0, meaning "the single
// subtree named by r.subtree".
type Register struct {
	Timeout Timeout
	Subtree ObjectIdentifier
}

// Type returns the pdu packet type.
func (r *Register) Type() Type {
	return TypeRegister
}

// MarshalBinary returns the pdu packet as a slice of bytes.
//
// RFC 2741 6.2.3 lays the payload out as r.timeout, r.priority, r.range_subid
// and one reserved byte, followed by r.subtree.
func (r *Register) MarshalBinary() ([]byte, error) {
	timeoutBytes, err := r.Timeout.MarshalBinary()
	if err != nil {
		return nil, err
	}

	subtreeBytes, err := r.Subtree.MarshalBinary()
	if err != nil {
		return nil, err
	}

	return append(timeoutBytes, subtreeBytes...), nil
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
//
// A subagent never receives an agentx-Register-PDU; this exists so the type
// satisfies Packet and so tests can round-trip what the library encodes.
func (r *Register) UnmarshalBinary(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("register: short buffer: got %d bytes, want at least 4", len(data))
	}
	if err := r.Timeout.UnmarshalBinary(data); err != nil {
		return err
	}
	if data[2] != 0 {
		return fmt.Errorf("register: range_subid %d is not supported", data[2])
	}
	return r.Subtree.UnmarshalBinary(data[4:])
}
