// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import "encoding/binary"

// Ranges defines the pdu search range list packet.
type Ranges []Range

// MarshalBinary returns the pdu packet as a slice of bytes.
func (r *Ranges) MarshalBinary() ([]byte, error) {
	result := []byte{}
	for index := range *r {
		data, err := (*r)[index].MarshalBinary()
		if err != nil {
			return nil, err
		}
		result = append(result, data...)
	}
	return result, nil
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
func (r *Ranges) UnmarshalBinary(data []byte) error {
	return r.UnmarshalBinaryOrder(data, binary.LittleEndian)
}

// UnmarshalBinaryOrder sets the packet structure from the provided slice of
// bytes, decoding multi-byte fields in the byte order the enclosing PDU header
// declared.
func (r *Ranges) UnmarshalBinaryOrder(data []byte, order binary.ByteOrder) error {
	*r = make([]Range, 0)
	for offset := 0; offset < len(data); {
		rng := Range{}
		if err := rng.UnmarshalBinaryOrder(data[offset:], order); err != nil {
			return err
		}
		*r = append(*r, rng)
		offset += rng.ByteSize()
	}
	return nil
}
