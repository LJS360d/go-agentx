// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import "errors"

// AllocateIndex defines the pdu allocate index packet.
type AllocateIndex struct {
	Variables Variables
}

// Type returns the pdu packet type.
func (ai *AllocateIndex) Type() Type {
	return TypeIndexAllocate
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (ai *AllocateIndex) MarshalBinary() ([]byte, error) {
	data, err := ai.Variables.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return data, nil
}

// UnmarshalBinary is not implemented.
//
// Index allocation is not supported by this library: nothing here sends an
// agentx-AllocateIndex-PDU, and the NEW_INDEX/ANY_INDEX flags of RFC 2741
// 6.2.12 are not exposed. Returning an error rather than silently succeeding
// keeps that visible.
func (ai *AllocateIndex) UnmarshalBinary(data []byte) error {
	return errors.New("allocate index: decoding is not implemented")
}
