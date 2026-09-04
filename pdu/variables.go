// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import (
	"encoding/binary"
	"strings"

	"github.com/LJS360d/go-agentx/value"
)

// Variables defines a list of variable bindings.
type Variables []Variable

// Add adds the provided variable.
func (v *Variables) Add(oid value.OID, t VariableType, value interface{}) {
	variable := Variable{}
	variable.Set(oid, t, value)
	*v = append(*v, variable)
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (v *Variables) MarshalBinary() ([]byte, error) {
	result := []byte{}
	for index := range *v {
		data, err := (*v)[index].MarshalBinary()
		if err != nil {
			return nil, err
		}
		result = append(result, data...)
	}
	return result, nil
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
func (v *Variables) UnmarshalBinary(data []byte) error {
	return v.UnmarshalBinaryOrder(data, binary.LittleEndian)
}

// UnmarshalBinaryOrder sets the packet structure from the provided slice of
// bytes, decoding multi-byte fields in the byte order the enclosing PDU header
// declared.
func (v *Variables) UnmarshalBinaryOrder(data []byte, order binary.ByteOrder) error {
	*v = make([]Variable, 0)
	for offset := 0; offset < len(data); {
		variable := Variable{}
		size, err := variable.unmarshal(data[offset:], order)
		if err != nil {
			return err
		}
		*v = append(*v, variable)
		offset += size
	}
	return nil
}

func (v Variables) String() string {
	parts := make([]string, len(v))
	for index, va := range v {
		parts[index] = va.String()
	}
	return "[variables " + strings.Join(parts, ", ") + "]"
}
