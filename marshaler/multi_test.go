// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package marshaler_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/LJS360d/go-agentx/marshaler"
)

type stub struct {
	data []byte
	err  error
}

func (s stub) MarshalBinary() ([]byte, error) { return s.data, s.err }

func TestMultiConcatenates(t *testing.T) {
	got, err := marshaler.NewMulti(stub{data: []byte{1, 2}}, stub{data: []byte{3}}).MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	if !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("MarshalBinary = % x, want 01 02 03", got)
	}

	if got, err := marshaler.NewMulti().MarshalBinary(); err != nil || len(got) != 0 {
		t.Fatalf("empty Multi = % x, %v", got, err)
	}
}

// A failing child must abort the whole concatenation rather than contribute a
// silent gap to the encoded PDU.
func TestMultiPropagatesErrors(t *testing.T) {
	want := errors.New("boom")
	got, err := marshaler.NewMulti(stub{data: []byte{1}}, stub{err: want}).MarshalBinary()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if got != nil {
		t.Fatalf("MarshalBinary returned % x alongside an error", got)
	}
}
