// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import (
	"fmt"
	"time"
)

// Open defines a pdu open packet.
type Open struct {
	Timeout     Timeout
	ID          ObjectIdentifier
	Description OctetString
}

// Type returns the pdu packet type.
func (o *Open) Type() Type {
	return TypeOpen
}

// MarshalBinary returns the pdu packet as a slice of bytes.
//
// RFC 2741 6.2.1 lays the payload out as o.timeout in one byte followed by
// three reserved bytes that must be zero-filled - the Timeout type's priority
// byte has no place here, it belongs to the Register PDU only.
func (o *Open) MarshalBinary() ([]byte, error) {
	if o.Timeout.Duration < 0 || o.Timeout.Duration > MaxTimeout {
		return nil, fmt.Errorf("open: timeout %s is not in [0s, %s]", o.Timeout.Duration, MaxTimeout)
	}

	result := []byte{byte(o.Timeout.Duration / time.Second), 0x00, 0x00, 0x00}

	idBytes, err := o.ID.MarshalBinary()
	if err != nil {
		return nil, err
	}
	result = append(result, idBytes...)

	descriptionBytes, err := o.Description.MarshalBinary()
	if err != nil {
		return nil, err
	}

	return append(result, descriptionBytes...), nil
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
//
// A subagent never receives an agentx-Open-PDU; this exists so the type
// satisfies Packet and so tests can round-trip what the library encodes.
func (o *Open) UnmarshalBinary(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("open: short buffer: got %d bytes, want at least 4", len(data))
	}
	o.Timeout.Duration = time.Duration(data[0]) * time.Second
	o.Timeout.Priority = 0

	if err := o.ID.UnmarshalBinary(data[4:]); err != nil {
		return err
	}
	return o.Description.UnmarshalBinary(data[4+o.ID.ByteSize():])
}
