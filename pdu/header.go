// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const (
	// HeaderSize defines the total size of a header packet.
	HeaderSize = 20

	// Version is the AgentX protocol version this library speaks (RFC 2741 6.1).
	Version = 1

	// MaxPayloadLength bounds the payload a peer may announce in h.payload_length.
	// The RFC does not cap it, but the receiver allocates that many bytes before
	// reading a single one of them, so an unbounded value is a one-packet
	// out-of-memory. No legitimate AgentX PDU comes close to this.
	MaxPayloadLength = 1 << 20
)

// Header defines a pdu packet header
type Header struct {
	Version       byte
	Type          Type
	Flags         Flags
	SessionID     uint32
	TransactionID uint32
	PacketID      uint32
	PayloadLength uint32
}

// ByteOrder resolves the RFC 2741 6.1 network-byte-order flag to the concrete
// encoding: set means big-endian ("network byte order"), unset means
// little-endian. Per RFC 2741 5 the flag governs every multi-byte integer in
// the PDU, both in the header and in the payload that follows it.
func ByteOrder(f Flags) binary.ByteOrder {
	if f&FlagNetworkByteOrder != 0 {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

// ByteOrder returns the byte order this header declares for itself and for its
// payload.
func (h *Header) ByteOrder() binary.ByteOrder {
	return ByteOrder(h.Flags)
}

// MarshalBinary returns the pdu header as a slice of bytes.
func (h *Header) MarshalBinary() ([]byte, error) {
	order := ByteOrder(h.Flags)
	buffer := bytes.NewBuffer([]byte{h.Version, byte(h.Type), byte(h.Flags), 0x00})

	binary.Write(buffer, order, h.SessionID)
	binary.Write(buffer, order, h.TransactionID)
	binary.Write(buffer, order, h.PacketID)
	binary.Write(buffer, order, h.PayloadLength)

	return buffer.Bytes(), nil
}

// UnmarshalBinary sets the header structure from the provided slice of bytes.
//
// RFC 2741 6.1: the network-byte-order flag is per-PDU and is set by the
// sender, so the four multi-byte header fields must be decoded in whatever
// order that specific packet declares, not a hardcoded one. The single-byte
// Flags field is read first to make that possible.
func (h *Header) UnmarshalBinary(data []byte) error {
	if len(data) < HeaderSize {
		return fmt.Errorf("not enough bytes (%d) to unmarshal the header (%d)", len(data), HeaderSize)
	}

	h.Version, h.Type, h.Flags = data[0], Type(data[1]), Flags(data[2])
	order := ByteOrder(h.Flags)

	buffer := bytes.NewBuffer(data[4:])

	binary.Read(buffer, order, &h.SessionID)
	binary.Read(buffer, order, &h.TransactionID)
	binary.Read(buffer, order, &h.PacketID)
	binary.Read(buffer, order, &h.PayloadLength)

	return h.Validate()
}

// Validate reports whether the decoded header is one this library is willing to
// act on. A header that fails this check is answered with parseError rather
// than processed (RFC 2741 7.2.2).
func (h *Header) Validate() error {
	if h.Version != Version {
		return fmt.Errorf("unsupported agentx version %d (want %d)", h.Version, Version)
	}
	// RFC 2741 6.1: "As a result of the encoding schemes and PDU layouts, this
	// value is always either 0, or a multiple of 4."
	if h.PayloadLength%4 != 0 {
		return fmt.Errorf("payload length %d is not a multiple of 4", h.PayloadLength)
	}
	if h.PayloadLength > MaxPayloadLength {
		return fmt.Errorf("payload length %d exceeds the maximum of %d", h.PayloadLength, MaxPayloadLength)
	}
	return nil
}

func (h *Header) String() string {
	return "(header " + h.Type.String() + ")"
}
