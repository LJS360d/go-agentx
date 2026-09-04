// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import (
	"encoding/binary"
	"fmt"
)

// Range defines the pdu search range packet.
type Range struct {
	From ObjectIdentifier
	To   ObjectIdentifier
}

// ByteSize returns the number of bytes, the binding would need in the encoded version.
func (r *Range) ByteSize() int {
	return r.From.ByteSize() + r.To.ByteSize()
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (r *Range) MarshalBinary() ([]byte, error) {
	fromBytes, err := r.From.MarshalBinary()
	if err != nil {
		return nil, err
	}

	// RFC 2741 5.2: the ending OID's include field is always 0.
	to := r.To
	to.Include = 0x00
	toBytes, err := to.MarshalBinary()
	if err != nil {
		return nil, err
	}

	return append(fromBytes, toBytes...), nil
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
func (r *Range) UnmarshalBinary(data []byte) error {
	return r.UnmarshalBinaryOrder(data, binary.LittleEndian)
}

// UnmarshalBinaryOrder sets the packet structure from the provided slice of
// bytes, decoding multi-byte fields in the byte order the enclosing PDU header
// declared.
func (r *Range) UnmarshalBinaryOrder(data []byte, order binary.ByteOrder) error {
	if err := r.From.UnmarshalBinaryOrder(data, order); err != nil {
		return err
	}
	if err := r.To.UnmarshalBinaryOrder(data[r.From.ByteSize():], order); err != nil {
		return err
	}
	return nil
}

func (r Range) String() string {
	result := ""
	if r.From.GetInclude() {
		result += "["
	} else {
		result += "("
	}
	result += fmt.Sprintf("%v, %v", r.From, r.To)
	if r.To.GetInclude() {
		result += "]"
	} else {
		result += ")"
	}
	return result
}
