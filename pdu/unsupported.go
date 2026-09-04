// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

// Unsupported is a placeholder packet for any PDU type this library does not
// implement (e.g. GetBulk, Ping, IndexAllocate, or any future type unknown to
// this version). It decodes by discarding its payload, which keeps the wire
// framing in sync, and lets Session.handle answer with genErr (RFC 2741
// 7.2.4.1: an agent that cannot process a request responds with an error)
// instead of the receiver dereferencing a nil packet and crashing.
type Unsupported struct {
	PacketType Type
}

// Type returns the pdu packet type that was actually received on the wire.
func (u *Unsupported) Type() Type {
	return u.PacketType
}

// MarshalBinary is not meaningful for an unsupported packet; it is never sent.
func (u *Unsupported) MarshalBinary() ([]byte, error) {
	return []byte{}, nil
}

// UnmarshalBinary discards the payload; there is nothing this library knows
// how to interpret for a type it does not implement.
func (u *Unsupported) UnmarshalBinary(data []byte) error {
	return nil
}
