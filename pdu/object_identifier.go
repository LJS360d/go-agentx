// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/LJS360d/go-agentx/value"
)

// MaxSubidentifiers is the number of sub-identifiers an Object Identifier may
// carry (RFC 2741 5.1: "The number (0-128) of sub-identifiers"). The count is
// encoded in a single byte, so anything above this cannot be represented and
// must be rejected rather than silently truncated.
const MaxSubidentifiers = 128

// ObjectIdentifier defines the pdu object identifier packet.
type ObjectIdentifier struct {
	Prefix         uint8
	Include        byte
	Subidentifiers []uint32
}

// SetInclude sets the include field.
func (o *ObjectIdentifier) SetInclude(value bool) {
	if value {
		o.Include = 0x01
	} else {
		o.Include = 0x00
	}
}

// GetInclude returns true if the include field ist set, false otherwise.
func (o *ObjectIdentifier) GetInclude() bool {
	return o.Include != 0x00
}

// SetIdentifier set the subidentifiers by the provided oid string.
//
// The RFC 2741 5.1 "prefix" compression (encoding 1.3.6.1.x as a prefix byte)
// is not applied on the encoding side; Prefix is left at 0, which the RFC
// defines as "no prefix". Decoding honours a prefix sent by the master agent.
func (o *ObjectIdentifier) SetIdentifier(oid value.OID) {
	o.Subidentifiers = make([]uint32, 0, len(oid))
	o.Subidentifiers = append(o.Subidentifiers, oid...)
}

// GetIdentifier returns the identifier as an oid string.
func (o *ObjectIdentifier) GetIdentifier() value.OID {
	var oid value.OID
	if o.Prefix != 0 {
		oid = append(oid, 1, 3, 6, 1, uint32(o.Prefix))
	}
	return append(oid, o.Subidentifiers...)
}

// ByteSize returns the number of bytes, the binding would need in the encoded version.
func (o *ObjectIdentifier) ByteSize() int {
	return 4 + len(o.Subidentifiers)*4
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (o *ObjectIdentifier) MarshalBinary() ([]byte, error) {
	if len(o.Subidentifiers) > MaxSubidentifiers {
		return nil, fmt.Errorf("object identifier: %d sub-identifiers exceeds the maximum of %d",
			len(o.Subidentifiers), MaxSubidentifiers)
	}

	buffer := bytes.NewBuffer([]byte{byte(len(o.Subidentifiers)), o.Prefix, o.Include, 0x00})

	for _, subidentifier := range o.Subidentifiers {
		binary.Write(buffer, binary.LittleEndian, &subidentifier)
	}

	return buffer.Bytes(), nil
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
func (o *ObjectIdentifier) UnmarshalBinary(data []byte) error {
	return o.UnmarshalBinaryOrder(data, binary.LittleEndian)
}

// UnmarshalBinaryOrder sets the packet structure from the provided slice of
// bytes, decoding the sub-identifiers in the byte order the enclosing PDU
// header declared (RFC 2741 5.1).
func (o *ObjectIdentifier) UnmarshalBinaryOrder(data []byte, order binary.ByteOrder) error {
	if len(data) < 4 {
		return fmt.Errorf("object identifier: short buffer: got %d bytes, want at least 4", len(data))
	}
	count := int(data[0])
	if count > MaxSubidentifiers {
		return fmt.Errorf("object identifier: %d sub-identifiers exceeds the maximum of %d",
			count, MaxSubidentifiers)
	}
	if len(data) < 4+count*4 {
		return fmt.Errorf("object identifier: short buffer: got %d bytes, want %d for %d sub-identifiers",
			len(data), 4+count*4, count)
	}

	o.Prefix = data[1]
	o.Include = data[2]

	o.Subidentifiers = make([]uint32, count)
	for index := 0; index < count; index++ {
		o.Subidentifiers[index] = order.Uint32(data[4+index*4:])
	}

	return nil
}

func (o ObjectIdentifier) String() string {
	return o.GetIdentifier().String()
}
