// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"
)

// responseHeaderSize is the fixed part of a response payload: res.sysUpTime,
// res.error and res.index (RFC 2741 6.2.16).
const responseHeaderSize = 8

// timeTick is the unit of sysUpTime and of the TimeTicks syntax: one hundredth
// of a second.
const timeTick = 10 * time.Millisecond

// Response defines the pdu response packet.
type Response struct {
	UpTime    time.Duration
	Error     Error
	Index     uint16
	Variables Variables
}

// Type returns the pdu packet type.
func (r *Response) Type() Type {
	return TypeResponse
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (r *Response) MarshalBinary() ([]byte, error) {
	buffer := &bytes.Buffer{}

	// RFC 2741 6.2.16: res.sysUpTime is expressed in hundredths of a second.
	upTime := uint32(r.UpTime / timeTick)
	binary.Write(buffer, binary.LittleEndian, &upTime)
	binary.Write(buffer, binary.LittleEndian, &r.Error)
	binary.Write(buffer, binary.LittleEndian, &r.Index)

	vBytes, err := r.Variables.MarshalBinary()
	if err != nil {
		return nil, err
	}
	buffer.Write(vBytes)

	return buffer.Bytes(), nil
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
func (r *Response) UnmarshalBinary(data []byte) error {
	return r.UnmarshalBinaryOrder(data, binary.LittleEndian)
}

// UnmarshalBinaryOrder sets the packet structure from the provided slice of
// bytes, decoding multi-byte fields in the byte order the header declared.
func (r *Response) UnmarshalBinaryOrder(data []byte, order binary.ByteOrder) error {
	if len(data) < responseHeaderSize {
		return fmt.Errorf("response: short buffer: got %d bytes, want at least %d", len(data), responseHeaderSize)
	}

	r.UpTime = time.Duration(order.Uint32(data)) * timeTick
	r.Error = Error(order.Uint16(data[4:]))
	r.Index = order.Uint16(data[6:])

	return r.Variables.UnmarshalBinaryOrder(data[responseHeaderSize:], order)
}

func (r *Response) String() string {
	return "(response " + r.Variables.String() + ")"
}
