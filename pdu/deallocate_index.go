// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import "errors"

// DeallocateIndex defines the pdu deallocate index packet.
type DeallocateIndex struct {
	Variables Variables
}

// Type returns the pdu packet type.
func (di *DeallocateIndex) Type() Type {
	return TypeIndexDeallocate
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (di *DeallocateIndex) MarshalBinary() ([]byte, error) {
	data, err := di.Variables.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return data, nil
}

// UnmarshalBinary is not implemented.
//
// Index allocation is not supported by this library: nothing here sends an
// agentx-DeallocateIndex-PDU, and the NEW_INDEX/ANY_INDEX flags of RFC 2741
// 6.2.12 are not exposed. Returning an error rather than silently succeeding
// keeps that visible.
func (di *DeallocateIndex) UnmarshalBinary(data []byte) error {
	return errors.New("deallocate index: decoding is not implemented")
}
