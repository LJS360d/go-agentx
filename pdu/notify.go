// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import (
	"bytes"
	"encoding/binary"
	"time"
)

// Notify defines the pdu notify packet (used for traps).
type Notify struct {
	Timestamp time.Duration
	Variables Variables
}

// Type returns the pdu packet type.
func (n *Notify) Type() Type {
	return TypeNotify
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (n *Notify) MarshalBinary() ([]byte, error) {
	buffer := &bytes.Buffer{}

	upTime := uint32(n.Timestamp.Seconds() * 100)
	binary.Write(buffer, binary.LittleEndian, &upTime)

	varsBytes, err := n.Variables.MarshalBinary()
	if err != nil {
		return nil, err
	}
	buffer.Write(varsBytes)

	return buffer.Bytes(), nil
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
func (n *Notify) UnmarshalBinary(data []byte) error {
	buffer := bytes.NewBuffer(data)

	timestamp := uint32(0)
	if err := binary.Read(buffer, binary.LittleEndian, &timestamp); err != nil {
		return err
	}
	n.Timestamp = time.Duration(timestamp) * time.Second / 100

	return n.Variables.UnmarshalBinary(data[4:])
}
