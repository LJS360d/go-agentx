// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import (
	"fmt"
	"time"
)

// MaxTimeout is the longest timeout representable on the wire. RFC 2741 6.2.1
// and 6.2.3 encode o.timeout and r.timeout as a single byte holding a number
// of seconds.
const MaxTimeout = 255 * time.Second

// Timeout defines the pdu timeout packet.
type Timeout struct {
	Duration time.Duration
	Priority byte
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (t *Timeout) MarshalBinary() ([]byte, error) {
	if t.Duration < 0 {
		return nil, fmt.Errorf("timeout: negative duration %s", t.Duration)
	}
	if t.Duration > MaxTimeout {
		return nil, fmt.Errorf("timeout: %s exceeds the maximum of %s encodable in one byte", t.Duration, MaxTimeout)
	}
	return []byte{byte(t.Duration / time.Second), t.Priority, 0x00, 0x00}, nil
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
func (t *Timeout) UnmarshalBinary(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("timeout: short buffer: got %d bytes, want 4", len(data))
	}
	t.Duration = time.Duration(data[0]) * time.Second
	t.Priority = data[1]
	return nil
}

func (t Timeout) String() string {
	return t.Duration.String()
}
