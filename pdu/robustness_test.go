// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/LJS360d/go-agentx/value"
)

// decoders are the entry points a master agent's bytes reach. Everything a
// subagent parses arrives from the network, so none of these may panic, hang,
// or allocate without bound on any input at all.
var decoders = map[string]func([]byte) error{
	"Header":           func(d []byte) error { return (&Header{}).UnmarshalBinary(d) },
	"ObjectIdentifier": func(d []byte) error { return (&ObjectIdentifier{}).UnmarshalBinary(d) },
	"OctetString":      func(d []byte) error { return (&OctetString{}).UnmarshalBinary(d) },
	"Range":            func(d []byte) error { return (&Range{}).UnmarshalBinary(d) },
	"Ranges":           func(d []byte) error { return (&Ranges{}).UnmarshalBinary(d) },
	"Variable":         func(d []byte) error { return (&Variable{}).UnmarshalBinary(d) },
	"Variables":        func(d []byte) error { return (&Variables{}).UnmarshalBinary(d) },
	"Response":         func(d []byte) error { return (&Response{}).UnmarshalBinary(d) },
	"Notify":           func(d []byte) error { return (&Notify{}).UnmarshalBinary(d) },
	"Get":              func(d []byte) error { return (&Get{}).UnmarshalBinary(d) },
	"GetNext":          func(d []byte) error { return (&GetNext{}).UnmarshalBinary(d) },
	"TestSet":          func(d []byte) error { return (&TestSet{}).UnmarshalBinary(d) },
	"CommitSet":        func(d []byte) error { return (&CommitSet{}).UnmarshalBinary(d) },
	"UndoSet":          func(d []byte) error { return (&UndoSet{}).UnmarshalBinary(d) },
	"CleanupSet":       func(d []byte) error { return (&CleanupSet{}).UnmarshalBinary(d) },
	"Close":            func(d []byte) error { return (&Close{}).UnmarshalBinary(d) },
	"Timeout":          func(d []byte) error { return (&Timeout{}).UnmarshalBinary(d) },
	"Open":             func(d []byte) error { return (&Open{}).UnmarshalBinary(d) },
	"Register":         func(d []byte) error { return (&Register{}).UnmarshalBinary(d) },
	"Unregister":       func(d []byte) error { return (&Unregister{}).UnmarshalBinary(d) },
}

// wellFormed returns a valid encoding of each shape, used as the seed corpus
// and as the basis for the truncation sweep.
func wellFormed(t testing.TB) map[string][]byte {
	t.Helper()

	header := &Header{Version: 1, Type: TypeResponse, SessionID: 1, TransactionID: 2, PacketID: 3, PayloadLength: 8}
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		t.Fatalf("header: %v", err)
	}

	var variables Variables
	variables.Add(value.OID{1, 3, 6, 1, 2}, VariableTypeOctetString, "unaligned text")
	variables.Add(value.OID{1, 3, 6, 1, 3}, VariableTypeCounter64, uint64(1<<40))
	variables.Add(value.OID{1, 3, 6, 1, 4}, VariableTypeObjectIdentifier, "1.2.3.4")
	variables.Add(value.OID{1, 3, 6, 1, 5}, VariableTypeNull, nil)
	variableBytes, err := variables.MarshalBinary()
	if err != nil {
		t.Fatalf("variables: %v", err)
	}

	ranges := Ranges{
		{From: oid(1, 3, 6, 1), To: oid(1, 3, 6, 2)},
		{From: oid(1, 3, 6, 3), To: ObjectIdentifier{}},
	}
	rangeBytes, err := ranges.MarshalBinary()
	if err != nil {
		t.Fatalf("ranges: %v", err)
	}

	response := &Response{UpTime: time.Second, Error: ErrorNone, Index: 0, Variables: variables}
	responseBytes, err := response.MarshalBinary()
	if err != nil {
		t.Fatalf("response: %v", err)
	}

	notify := &Notify{Timestamp: time.Second}
	notify.Variables.Add(OIDSnmpTrapOID, VariableTypeObjectIdentifier, "1.3.6.1.4.1.42.0.1")
	notifyBytes, err := notify.MarshalBinary()
	if err != nil {
		t.Fatalf("notify: %v", err)
	}

	open := &Open{}
	open.Timeout.Duration = 5 * time.Second
	open.ID.SetIdentifier(value.OID{1, 3, 6, 1})
	open.Description.Text = "seed"
	openBytes, err := open.MarshalBinary()
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	register := &Register{Subtree: oid(1, 3, 6, 1)}
	register.Timeout.Duration = 5 * time.Second
	registerBytes, err := register.MarshalBinary()
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	return map[string][]byte{
		"Header":           headerBytes,
		"ObjectIdentifier": le32([]byte{4, 2, 0, 0}, 1, 1, 1, 0),
		"OctetString":      append(le32(nil, 5), 'a', 'b', 'c', 'd', 'e', 0, 0, 0),
		"Range":            rangeBytes[:ranges[0].ByteSize()],
		"Ranges":           rangeBytes,
		"Variable":         variableBytes[:variables[0].ByteSize()],
		"Variables":        variableBytes,
		"Response":         responseBytes,
		"Notify":           notifyBytes,
		"Get":              rangeBytes,
		"GetNext":          rangeBytes,
		"TestSet":          variableBytes,
		"CommitSet":        {},
		"UndoSet":          {},
		"CleanupSet":       {},
		"Close":            {byte(ReasonShutdown), 0, 0, 0},
		"Timeout":          {5, 127, 0, 0},
		"Open":             openBytes,
		"Register":         registerBytes,
		"Unregister":       {0, 127, 0, 0, 4, 0, 0, 0, 1, 0, 0, 0, 3, 0, 0, 0, 6, 0, 0, 0, 1, 0, 0, 0},
	}
}

// A truncated PDU is what a subagent sees when a master agent is killed
// mid-write, or when a hostile peer sends one deliberately. Every prefix of
// every valid encoding must come back as an error or a shorter valid value -
// never a panic. The old decoders sliced on lengths read straight off the wire
// (data[4:4+length], data[0], remaining[offset:]) and panicked on all of these.
func TestDecodersSurviveTruncation(t *testing.T) {
	for name, data := range wellFormed(t) {
		decode, ok := decoders[name]
		if !ok {
			t.Fatalf("no decoder registered for %s", name)
		}

		t.Run(name, func(t *testing.T) {
			for i := 0; i < len(data); i++ {
				prefix := data[:i]
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("panic decoding the first %d of %d bytes: %v", i, len(data), r)
						}
					}()
					_ = decode(prefix)
				}()
			}
		})
	}
}

// The same sweep, but corrupting one byte at a time rather than truncating:
// this is what reaches the length and count fields that drive every slice.
func TestDecodersSurviveCorruption(t *testing.T) {
	for name, data := range wellFormed(t) {
		decode := decoders[name]

		t.Run(name, func(t *testing.T) {
			for i := 0; i < len(data); i++ {
				for _, b := range []byte{0x00, 0x01, 0x7F, 0x80, 0xFF} {
					corrupted := append([]byte(nil), data...)
					corrupted[i] = b
					func() {
						defer func() {
							if r := recover(); r != nil {
								t.Fatalf("panic decoding with byte %d set to %#x: %v", i, b, r)
							}
						}()
						_ = decode(corrupted)
					}()
				}
			}
		})
	}
}

// Explicit short-buffer expectations, so that "no panic" cannot be satisfied
// by a decoder that quietly accepts nonsense.
func TestDecodersRejectShortBuffers(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"Header", make([]byte, HeaderSize-1)},
		{"ObjectIdentifier", []byte{4, 0, 0}},
		{"ObjectIdentifier", []byte{4, 0, 0, 0, 1, 0, 0, 0}}, // claims 4 subids, carries 1
		{"OctetString", []byte{4, 0, 0}},
		{"Variable", []byte{2, 0, 0}},
		{"Variable", []byte{2, 0, 0, 0, 0, 0, 0, 0}}, // integer varbind with no value bytes
		{"Response", make([]byte, 7)},
		{"Close", []byte{5, 0, 0}},
		{"Timeout", []byte{5, 0, 0}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := decoders[c.name](c.data); err == nil {
				t.Fatalf("decoding % x succeeded, want a short-buffer error", c.data)
			}
		})
	}
}

// RFC 2741 6.1: h.payload_length "is always either 0, or a multiple of 4", and
// the receiver allocates that many bytes before reading any of them. An
// unbounded value is a one-packet out-of-memory.
func TestHeaderRejectsImplausiblePayloadLengths(t *testing.T) {
	build := func(version byte, payloadLength uint32) []byte {
		header := &Header{Version: version, Type: TypeGet, PayloadLength: payloadLength}
		data, err := header.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary: %v", err)
		}
		return data
	}

	cases := []struct {
		name string
		data []byte
	}{
		{"payload length not a multiple of 4", build(1, 5)},
		{"payload length above the maximum", build(1, MaxPayloadLength+4)},
		{"unsupported version", build(2, 0)},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := (&Header{}).UnmarshalBinary(c.data); err == nil {
				t.Fatal("UnmarshalBinary succeeded, want an error")
			}
		})
	}

	if err := (&Header{}).UnmarshalBinary(build(1, MaxPayloadLength)); err != nil {
		t.Fatalf("UnmarshalBinary rejected the maximum payload length: %v", err)
	}
}

// A rejected header still has to leave its fields decoded: the client uses the
// payload length of a header it refused in order to skip the payload and stay
// framed on the connection.
func TestHeaderPopulatesFieldsEvenWhenRejected(t *testing.T) {
	header := &Header{Version: 2, Type: TypeGet, SessionID: 9, PayloadLength: 8}
	data, err := header.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	decoded := &Header{}
	if err := decoded.UnmarshalBinary(data); err == nil {
		t.Fatal("UnmarshalBinary accepted version 2")
	}
	if decoded.PayloadLength != 8 || decoded.SessionID != 9 {
		t.Fatalf("decoded = %+v, want the fields populated despite the error", decoded)
	}
}

func FuzzDecoders(f *testing.F) {
	for _, data := range wellFormed(f) {
		f.Add(data)
	}
	f.Add([]byte{})
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF})

	f.Fuzz(func(t *testing.T, data []byte) {
		for _, decode := range decoders {
			// A panic here fails the fuzz target; that is the property under
			// test. Everything a subagent decodes is attacker-controlled.
			_ = decode(data)
		}
	})
}

// FuzzVariablesRoundTrip additionally checks that anything which decodes and
// re-encodes does so stably: a decode/encode pair that drifts would desync a
// VarBindList walk.
func FuzzVariablesRoundTrip(f *testing.F) {
	seeds := wellFormed(f)
	f.Add(seeds["Variables"])
	f.Add(seeds["Variable"])

	f.Fuzz(func(t *testing.T, data []byte) {
		var variables Variables
		if err := variables.UnmarshalBinary(data); err != nil {
			return
		}

		encoded, err := variables.MarshalBinary()
		if err != nil {
			return
		}

		var again Variables
		if err := again.UnmarshalBinary(encoded); err != nil {
			t.Fatalf("re-decoding a self-encoded VarBindList failed: %v", err)
		}
		if len(again) != len(variables) {
			t.Fatalf("re-decode produced %d variables, want %d", len(again), len(variables))
		}
	})
}

// FuzzHeaderByteOrder checks the property RFC 2741 6.1 puts on the header: the
// flags byte decides how the other fields are read, so a header must survive a
// round trip in whichever order it declares.
func FuzzHeaderByteOrder(f *testing.F) {
	f.Add(uint8(0), uint32(1), uint32(2), uint32(3), uint32(8))
	f.Add(uint8(FlagNetworkByteOrder), uint32(0xFFFFFFFF), uint32(0), uint32(1), uint32(0))

	f.Fuzz(func(t *testing.T, flags uint8, sessionID, transactionID, packetID, payloadLength uint32) {
		payloadLength = (payloadLength % (MaxPayloadLength + 1)) &^ 3

		in := &Header{
			Version:       Version,
			Type:          TypeResponse,
			Flags:         Flags(flags),
			SessionID:     sessionID,
			TransactionID: transactionID,
			PacketID:      packetID,
			PayloadLength: payloadLength,
		}

		data, err := in.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary: %v", err)
		}
		if len(data) != HeaderSize {
			t.Fatalf("encoded %d bytes, want %d", len(data), HeaderSize)
		}

		out := &Header{}
		if err := out.UnmarshalBinary(data); err != nil {
			t.Fatalf("UnmarshalBinary: %v", err)
		}
		if *out != *in {
			t.Fatalf("round trip changed the header:\n got %+v\nwant %+v", out, in)
		}

		// And the declared order really is the one used.
		order := ByteOrder(Flags(flags))
		if got := order.Uint32(data[4:]); got != sessionID {
			t.Fatalf("session id encoded as %d, want %d in %v order", got, sessionID, order)
		}
		if order == binary.BigEndian && Flags(flags)&FlagNetworkByteOrder == 0 {
			t.Fatal("big-endian selected without the network-byte-order flag")
		}
	})
}
