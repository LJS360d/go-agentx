// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// OctetString defines the pdu description packet.
type OctetString struct {
	Text string
}

// ByteSize returns the number of bytes the octet string needs in its encoded
// form: the 4-byte length, the octets themselves, and the zero padding that
// realigns the following data to a 4-byte offset (RFC 2741 5.3).
func (o *OctetString) ByteSize() int {
	return octetStringSize(len(o.Text))
}

func octetStringSize(textLength int) int {
	return 4 + textLength + padding(textLength)
}

// padding returns the number of zero bytes needed to bring length up to a
// multiple of 4. RFC 2741 5.3 requires the padding "even if the Octet String
// is the last item in the PDU".
func padding(length int) int {
	return (4 - length%4) % 4
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (o *OctetString) MarshalBinary() ([]byte, error) {
	buffer := &bytes.Buffer{}

	binary.Write(buffer, binary.LittleEndian, uint32(len(o.Text)))
	buffer.WriteString(o.Text)

	for i := padding(len(o.Text)); i > 0; i-- {
		buffer.WriteByte(0x00)
	}

	return buffer.Bytes(), nil
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
func (o *OctetString) UnmarshalBinary(data []byte) error {
	return o.UnmarshalBinaryOrder(data, binary.LittleEndian)
}

// UnmarshalBinaryOrder sets the packet structure from the provided slice of
// bytes, decoding the length prefix in the byte order the enclosing PDU header
// declared (RFC 2741 5.3).
func (o *OctetString) UnmarshalBinaryOrder(data []byte, order binary.ByteOrder) error {
	if len(data) < 4 {
		return fmt.Errorf("octet string: short buffer: got %d bytes, want at least 4", len(data))
	}

	length := order.Uint32(data)
	// The length comes straight off the wire. Compare in uint64 so that neither
	// the conversion to int nor the addition below can wrap on a 32-bit build.
	if uint64(length) > uint64(len(data)-4) {
		return fmt.Errorf("octet string: length %d exceeds the %d bytes available", length, len(data)-4)
	}

	o.Text = string(data[4 : 4+length])

	return nil
}
