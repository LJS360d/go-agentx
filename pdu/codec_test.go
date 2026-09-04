// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/LJS360d/go-agentx/value"
)

// oid is shorthand for building an ObjectIdentifier from sub-identifiers.
func oid(subidentifiers ...uint32) ObjectIdentifier {
	o := ObjectIdentifier{}
	o.SetIdentifier(value.OID(subidentifiers))
	return o
}

// le32 appends v as four little-endian bytes, the byte order this library
// encodes in (it leaves NETWORK_BYTE_ORDER clear, RFC 2741 6.1).
func le32(data []byte, values ...uint32) []byte {
	for _, v := range values {
		data = binary.LittleEndian.AppendUint32(data, v)
	}
	return data
}

// TestObjectIdentifierGolden checks the encoding against the examples RFC 2741
// 5.1 spells out byte for byte.
func TestObjectIdentifierGolden(t *testing.T) {
	cases := []struct {
		name string
		in   ObjectIdentifier
		want []byte
		// oid is what GetIdentifier must report after a round trip.
		oid value.OID
	}{
		{
			name: "1.2.3.4",
			in:   oid(1, 2, 3, 4),
			want: le32([]byte{4, 0, 0, 0}, 1, 2, 3, 4),
			oid:  value.OID{1, 2, 3, 4},
		},
		{
			name: "null object identifier",
			in:   ObjectIdentifier{},
			want: []byte{0, 0, 0, 0},
			oid:  nil,
		},
		{
			name: "sysDescr.0 uncompressed",
			in:   oid(1, 3, 6, 1, 2, 1, 1, 1, 0),
			want: le32([]byte{9, 0, 0, 0}, 1, 3, 6, 1, 2, 1, 1, 1, 0),
			oid:  value.OID{1, 3, 6, 1, 2, 1, 1, 1, 0},
		},
		{
			name: "include flag set",
			in:   ObjectIdentifier{Include: 1, Subidentifiers: []uint32{1}},
			want: le32([]byte{1, 0, 1, 0}, 1),
			oid:  value.OID{1},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.in.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary: %v", err)
			}
			if !bytes.Equal(got, c.want) {
				t.Fatalf("MarshalBinary = % x, want % x", got, c.want)
			}

			var decoded ObjectIdentifier
			if err := decoded.UnmarshalBinary(got); err != nil {
				t.Fatalf("UnmarshalBinary: %v", err)
			}
			if decoded.GetIdentifier().String() != c.oid.String() {
				t.Fatalf("GetIdentifier = %v, want %v", decoded.GetIdentifier(), c.oid)
			}
			if decoded.ByteSize() != len(c.want) {
				t.Fatalf("ByteSize = %d, want %d", decoded.ByteSize(), len(c.want))
			}
		})
	}
}

// RFC 2741 5.1: a non-zero prefix byte "x" stands for the implicit prefix
// 1.3.6.1.x. The library does not emit prefixes, but a master agent does, and
// the sysDescr.0 example in that section is encoded with one.
func TestObjectIdentifierPrefixDecoding(t *testing.T) {
	// The RFC's own sysDescr.0 example: n_subid 4, prefix 2, then 1.1.1.0.
	data := le32([]byte{4, 2, 0, 0}, 1, 1, 1, 0)

	var decoded ObjectIdentifier
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}

	want := "1.3.6.1.2.1.1.1.0"
	if got := decoded.GetIdentifier().String(); got != want {
		t.Fatalf("GetIdentifier = %s, want %s", got, want)
	}
	if decoded.ByteSize() != len(data) {
		t.Fatalf("ByteSize = %d, want %d", decoded.ByteSize(), len(data))
	}
}

// RFC 2741 5.1 caps an object identifier at 128 sub-identifiers. The count is
// a single byte on the wire, so a longer OID cannot be encoded - and silently
// truncating the count (as byte(len(...)) did) produces a PDU that decodes as
// a completely different, shorter OID.
func TestObjectIdentifierRejectsTooManySubidentifiers(t *testing.T) {
	o := ObjectIdentifier{Subidentifiers: make([]uint32, 256)}
	if _, err := o.MarshalBinary(); err == nil {
		t.Fatal("MarshalBinary accepted 256 sub-identifiers, want an error")
	}

	o = ObjectIdentifier{Subidentifiers: make([]uint32, MaxSubidentifiers)}
	if _, err := o.MarshalBinary(); err != nil {
		t.Fatalf("MarshalBinary rejected the maximum of %d sub-identifiers: %v", MaxSubidentifiers, err)
	}
}

// TestOctetStringGolden pins the padding rules of RFC 2741 5.3: the length is
// the count of octets, and zero padding follows so that the next item starts
// on a 4-byte boundary - "even if the Octet String is the last item in the
// PDU".
func TestOctetStringGolden(t *testing.T) {
	cases := []struct {
		text string
		want []byte
	}{
		{"", []byte{0, 0, 0, 0}},
		{"a", append(le32(nil, 1), 'a', 0, 0, 0)},
		{"ab", append(le32(nil, 2), 'a', 'b', 0, 0)},
		{"abc", append(le32(nil, 3), 'a', 'b', 'c', 0)},
		{"abcd", append(le32(nil, 4), 'a', 'b', 'c', 'd')},
		{"abcde", append(le32(nil, 5), 'a', 'b', 'c', 'd', 'e', 0, 0, 0)},
	}

	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			in := &OctetString{Text: c.text}
			got, err := in.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary: %v", err)
			}
			if !bytes.Equal(got, c.want) {
				t.Fatalf("MarshalBinary = % x, want % x", got, c.want)
			}
			if len(got)%4 != 0 {
				t.Fatalf("encoded length %d is not 4-byte aligned", len(got))
			}
			if in.ByteSize() != len(c.want) {
				t.Fatalf("ByteSize = %d, want %d", in.ByteSize(), len(c.want))
			}

			var decoded OctetString
			if err := decoded.UnmarshalBinary(got); err != nil {
				t.Fatalf("UnmarshalBinary: %v", err)
			}
			if decoded.Text != c.text {
				t.Fatalf("Text = %q, want %q", decoded.Text, c.text)
			}
		})
	}
}

// A length field that claims more octets than the buffer holds must be an
// error. Slicing on it without checking - data[4:4+length] - panics, and the
// length comes straight off the network.
func TestOctetStringRejectsOverlongLength(t *testing.T) {
	data := append(le32(nil, 0xFFFFFFFF), 'a', 'b', 'c', 'd')

	var decoded OctetString
	if err := decoded.UnmarshalBinary(data); err == nil {
		t.Fatal("UnmarshalBinary accepted a length larger than the buffer, want an error")
	}
}

// TestSearchRangeGolden uses the worked example in RFC 2741 5.2: the range
// from 1.3.6.1.2.1.25.2 inclusive to 1.3.6.1.2.1.25.2.1 exclusive, both
// encoded with the 1.3.6.1.2 prefix.
func TestSearchRangeGolden(t *testing.T) {
	data := le32([]byte{3, 2, 1, 0}, 1, 25, 2)
	data = append(data, le32([]byte{4, 2, 0, 0}, 1, 25, 2, 1)...)

	var r Range
	if err := r.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}

	if got, want := r.From.GetIdentifier().String(), "1.3.6.1.2.1.25.2"; got != want {
		t.Fatalf("From = %s, want %s", got, want)
	}
	if !r.From.GetInclude() {
		t.Fatal("From.include = false, want true")
	}
	if got, want := r.To.GetIdentifier().String(), "1.3.6.1.2.1.25.2.1"; got != want {
		t.Fatalf("To = %s, want %s", got, want)
	}
	if r.To.GetInclude() {
		t.Fatal("To.include = true; RFC 2741 5.2 says the ending OID's include field is always 0")
	}
	if r.ByteSize() != len(data) {
		t.Fatalf("ByteSize = %d, want %d", r.ByteSize(), len(data))
	}
}

// RFC 2741 5.2: the ending OID of a search range always has include 0, no
// matter what the caller set.
func TestRangeMarshalClearsEndingInclude(t *testing.T) {
	r := Range{From: oid(1), To: oid(2)}
	r.To.SetInclude(true)

	data, err := r.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	var decoded Range
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	if decoded.To.GetInclude() {
		t.Fatal("encoded ending OID has include set")
	}
	if !r.To.GetInclude() {
		t.Fatal("MarshalBinary mutated the caller's Range")
	}
}

// TestVariableRoundTrip covers every syntax RFC 2741 5.4 defines, in both byte
// orders. The values chosen are asymmetric so that a byte-order mistake cannot
// pass by accident.
func TestVariableRoundTrip(t *testing.T) {
	name := value.OID{1, 3, 6, 1, 4, 1, 42, 1, 0}

	cases := []struct {
		name  string
		vtype VariableType
		in    interface{}
		want  interface{}
		// dataSize is the number of encoded bytes after v.name.
		dataSize int
	}{
		{"integer", VariableTypeInteger, int32(-1234567), int32(-1234567), 4},
		{"integer min", VariableTypeInteger, int32(-2147483648), int32(-2147483648), 4},
		{"counter32", VariableTypeCounter32, uint32(0x01020304), uint32(0x01020304), 4},
		{"gauge32", VariableTypeGauge32, uint32(0xFFFFFFFF), uint32(0xFFFFFFFF), 4},
		{"counter64", VariableTypeCounter64, uint64(0x0102030405060708), uint64(0x0102030405060708), 8},
		{"timeticks", VariableTypeTimeTicks, 1234 * 10 * time.Millisecond, 1234 * 10 * time.Millisecond, 4},
		{"octet string", VariableTypeOctetString, "hello", "hello", 12},
		{"octet string empty", VariableTypeOctetString, "", "", 4},
		{"opaque", VariableTypeOpaque, []byte{1, 2, 3}, []byte{1, 2, 3}, 8},
		{"null", VariableTypeNull, nil, nil, 0},
		{"no such object", VariableTypeNoSuchObject, nil, nil, 0},
		{"no such instance", VariableTypeNoSuchInstance, nil, nil, 0},
		{"end of mib view", VariableTypeEndOfMIBView, nil, nil, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := Variable{}
			in.Set(name, c.vtype, c.in)

			data, err := in.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary: %v", err)
			}

			wantSize := variableHeaderSize + in.Name.ByteSize() + c.dataSize
			if len(data) != wantSize {
				t.Fatalf("encoded length = %d, want %d", len(data), wantSize)
			}
			if in.ByteSize() != wantSize {
				t.Fatalf("ByteSize = %d, want %d", in.ByteSize(), wantSize)
			}

			var decoded Variable
			consumed, err := decoded.unmarshal(data, binary.LittleEndian)
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if consumed != len(data) {
				t.Fatalf("consumed %d bytes, want %d", consumed, len(data))
			}
			if decoded.Type != c.vtype {
				t.Fatalf("Type = %v, want %v", decoded.Type, c.vtype)
			}
			if decoded.Name.GetIdentifier().String() != name.String() {
				t.Fatalf("Name = %v, want %v", decoded.Name.GetIdentifier(), name)
			}
			assertValueEqual(t, decoded.Value, c.want)
		})
	}
}

// RFC 2741 5: the NETWORK_BYTE_ORDER bit governs every multi-byte integer in
// the PDU, including the ones in the payload. Decoding a big-endian peer's
// varbinds as little-endian yields byte-swapped garbage.
func TestVariableBigEndianDecoding(t *testing.T) {
	name := oid(1, 3, 6, 1)

	// A Counter32 varbind, hand-encoded entirely in network byte order.
	data := make([]byte, 0, 16)
	data = binary.BigEndian.AppendUint16(data, uint16(VariableTypeCounter32))
	data = append(data, 0, 0)
	data = append(data, byte(len(name.Subidentifiers)), 0, 0, 0)
	for _, sub := range name.Subidentifiers {
		data = binary.BigEndian.AppendUint32(data, sub)
	}
	data = binary.BigEndian.AppendUint32(data, 0x01020304)

	var decoded Variable
	if err := decoded.UnmarshalBinaryOrder(data, binary.BigEndian); err != nil {
		t.Fatalf("UnmarshalBinaryOrder: %v", err)
	}
	if decoded.Type != VariableTypeCounter32 {
		t.Fatalf("Type = %v, want VariableTypeCounter32", decoded.Type)
	}
	if got, want := decoded.Name.GetIdentifier().String(), "1.3.6.1"; got != want {
		t.Fatalf("Name = %s, want %s", got, want)
	}
	if decoded.Value != uint32(0x01020304) {
		t.Fatalf("Value = %v, want %v", decoded.Value, uint32(0x01020304))
	}

	// The same bytes read as little-endian must not silently produce the same
	// answer, or this test proves nothing.
	var wrong Variable
	if err := wrong.UnmarshalBinaryOrder(data, binary.LittleEndian); err == nil && wrong.Value == decoded.Value {
		t.Fatal("little-endian decoding produced the same value; the byte order is being ignored")
	}
}

// An OID-valued varbind decodes into a value.OID. Re-encoding it used to panic
// on a type assertion to string, which took down any parse of a VarBindList
// containing one, because the list walked itself by re-marshalling.
func TestVariableObjectIdentifierRoundTrip(t *testing.T) {
	in := Variable{}
	in.Set(value.OID{1, 3, 6, 1}, VariableTypeObjectIdentifier, "1.3.6.1.4.1.42")

	data, err := in.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	var decoded Variable
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	got, ok := decoded.Value.(value.OID)
	if !ok {
		t.Fatalf("Value has type %T, want value.OID", decoded.Value)
	}
	if got.String() != "1.3.6.1.4.1.42" {
		t.Fatalf("Value = %s, want 1.3.6.1.4.1.42", got)
	}

	// The decoded form must be re-encodable, byte for byte.
	again, err := decoded.MarshalBinary()
	if err != nil {
		t.Fatalf("re-MarshalBinary: %v", err)
	}
	if !bytes.Equal(data, again) {
		t.Fatalf("re-encoding differs:\n got % x\nwant % x", again, data)
	}
}

// RFC 2741 5.4: an IpAddress is an octet string whose octets run most
// significant first. net.ParseIP hands back a 16-byte IPv4-in-IPv6 form for a
// v4 address, and encoding that verbatim puts 16 octets where a manager
// expects 4.
func TestVariableIPAddressIsFourOctets(t *testing.T) {
	for _, in := range []net.IP{net.ParseIP("10.1.2.3"), net.IPv4(10, 1, 2, 3), {10, 1, 2, 3}} {
		v := Variable{}
		v.Set(value.OID{1, 3, 6, 1}, VariableTypeIPAddress, in)

		data, err := v.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary(%v): %v", in, err)
		}

		var decoded Variable
		if err := decoded.UnmarshalBinary(data); err != nil {
			t.Fatalf("UnmarshalBinary: %v", err)
		}
		ip, ok := decoded.Value.(net.IP)
		if !ok {
			t.Fatalf("Value has type %T, want net.IP", decoded.Value)
		}
		if len(ip) != 4 {
			t.Fatalf("encoded %d octets for %v, want 4", len(ip), in)
		}
		if !ip.Equal(net.IPv4(10, 1, 2, 3)) {
			t.Fatalf("Value = %v, want 10.1.2.3", ip)
		}
	}

	v := Variable{}
	v.Set(value.OID{1, 3, 6, 1}, VariableTypeIPAddress, net.ParseIP("2001:db8::1"))
	if _, err := v.MarshalBinary(); err == nil {
		t.Fatal("MarshalBinary accepted an IPv6 address for an SNMP IpAddress")
	}
}

// A Handler is user code and can hand back a value of the wrong Go type. That
// has to surface as an error, not as a panic that takes the process with it.
func TestVariableMarshalRejectsMismatchedValues(t *testing.T) {
	cases := []struct {
		name  string
		vtype VariableType
		value interface{}
	}{
		{"integer given a string", VariableTypeInteger, "nope"},
		{"integer given an int", VariableTypeInteger, 42},
		{"counter32 given an int64", VariableTypeCounter32, int64(1)},
		{"counter64 given a uint32", VariableTypeCounter64, uint32(1)},
		{"timeticks given a string", VariableTypeTimeTicks, "nope"},
		{"timeticks given a negative duration", VariableTypeTimeTicks, -time.Second},
		{"octet string given an int", VariableTypeOctetString, 1},
		{"oid given an int", VariableTypeObjectIdentifier, 1},
		{"oid given an unparseable string", VariableTypeObjectIdentifier, "not.an.oid"},
		{"ip address given an int", VariableTypeIPAddress, 1},
		{"unknown type", VariableType(4242), nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := Variable{}
			v.Set(value.OID{1, 3, 6, 1}, c.vtype, c.value)

			if _, err := v.MarshalBinary(); err == nil {
				t.Fatal("MarshalBinary succeeded, want an error")
			}
			// ByteSize must stay panic-free for an unencodable variable.
			if size := v.ByteSize(); size <= 0 {
				t.Fatalf("ByteSize = %d, want a positive fallback", size)
			}
		})
	}
}

// A VarBindList is walked by consuming each binding's encoded length. Deriving
// that length by re-encoding the decoded value breaks for anything the library
// can decode but not encode, so the walk is driven by what was actually read.
func TestVariablesRoundTrip(t *testing.T) {
	var in Variables
	in.Add(value.OID{1, 3, 6, 1, 2}, VariableTypeInteger, int32(7))
	in.Add(value.OID{1, 3, 6, 1, 3}, VariableTypeOctetString, "unaligned")
	in.Add(value.OID{1, 3, 6, 1, 4}, VariableTypeObjectIdentifier, "1.2.3")
	in.Add(value.OID{1, 3, 6, 1, 5}, VariableTypeNull, nil)
	in.Add(value.OID{1, 3, 6, 1, 6}, VariableTypeCounter64, uint64(1<<40))

	data, err := in.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	var decoded Variables
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	if len(decoded) != len(in) {
		t.Fatalf("decoded %d variables, want %d", len(decoded), len(in))
	}
	for i := range decoded {
		if decoded[i].Name.GetIdentifier().String() != in[i].Name.GetIdentifier().String() {
			t.Fatalf("variable %d: name = %v, want %v", i, decoded[i].Name, in[i].Name)
		}
		if decoded[i].Type != in[i].Type {
			t.Fatalf("variable %d: type = %v, want %v", i, decoded[i].Type, in[i].Type)
		}
	}

	again, err := decoded.MarshalBinary()
	if err != nil {
		t.Fatalf("re-MarshalBinary: %v", err)
	}
	if !bytes.Equal(data, again) {
		t.Fatalf("re-encoding differs:\n got % x\nwant % x", again, data)
	}
}

// RFC 2741 6.2.16: res.sysUpTime is a count of hundredths of a second. The
// conversion has to survive a round trip in both directions - dividing where a
// multiplication belongs silently reports an uptime 10000x off.
func TestResponseUpTimeRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		in    time.Duration
		ticks uint32
	}{
		{"zero", 0, 0},
		{"one tick", 10 * time.Millisecond, 1},
		{"one second", time.Second, 100},
		{"one hour", time.Hour, 360000},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := &Response{UpTime: c.in, Error: ErrorNone}
			data, err := in.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary: %v", err)
			}
			if got := binary.LittleEndian.Uint32(data); got != c.ticks {
				t.Fatalf("encoded sysUpTime = %d hundredths, want %d", got, c.ticks)
			}

			var decoded Response
			if err := decoded.UnmarshalBinary(data); err != nil {
				t.Fatalf("UnmarshalBinary: %v", err)
			}
			if decoded.UpTime != c.in {
				t.Fatalf("UpTime = %s, want %s", decoded.UpTime, c.in)
			}
		})
	}
}

func TestResponseRoundTrip(t *testing.T) {
	in := &Response{UpTime: 42 * time.Second, Error: ErrorWrongValue, Index: 3}
	in.Variables.Add(value.OID{1, 3, 6, 1}, VariableTypeInteger, int32(9))

	data, err := in.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	var decoded Response
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	if decoded.Error != ErrorWrongValue {
		t.Fatalf("Error = %v, want ErrorWrongValue", decoded.Error)
	}
	if decoded.Index != 3 {
		t.Fatalf("Index = %d, want 3", decoded.Index)
	}
	if len(decoded.Variables) != 1 || decoded.Variables[0].Value != int32(9) {
		t.Fatalf("Variables = %v, want one integer varbind with value 9", decoded.Variables)
	}
}

// RFC 2741 6.2.10: an agentx-Notify-PDU is an optional context followed by a
// VarBindList. There is no timestamp field; encoding one shifts every varbind
// that follows and corrupts the notification.
func TestNotifyHasNoTimestampField(t *testing.T) {
	n := &Notify{Timestamp: 5 * time.Second}
	n.Variables.Add(OIDSnmpTrapOID, VariableTypeObjectIdentifier, "1.3.6.1.4.1.42.0.1")

	data, err := n.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	// The payload must parse as a bare VarBindList.
	var decoded Variables
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("payload does not parse as a VarBindList: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("decoded %d varbinds, want 2 (sysUpTime.0 and snmpTrapOID.0)", len(decoded))
	}

	// RFC 2741 6.2.10: sysUpTime.0 first, snmpTrapOID.0 second.
	if got := decoded[0].Name.GetIdentifier().String(); got != OIDSysUpTime.String() {
		t.Fatalf("first varbind = %s, want sysUpTime.0 (%s)", got, OIDSysUpTime)
	}
	if decoded[0].Type != VariableTypeTimeTicks {
		t.Fatalf("sysUpTime.0 type = %v, want VariableTypeTimeTicks", decoded[0].Type)
	}
	if decoded[0].Value != 5*time.Second {
		t.Fatalf("sysUpTime.0 = %v, want 5s", decoded[0].Value)
	}
	if got := decoded[1].Name.GetIdentifier().String(); got != OIDSnmpTrapOID.String() {
		t.Fatalf("second varbind = %s, want snmpTrapOID.0 (%s)", got, OIDSnmpTrapOID)
	}

	var roundTripped Notify
	if err := roundTripped.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	if roundTripped.Timestamp != 5*time.Second {
		t.Fatalf("Timestamp = %s, want 5s", roundTripped.Timestamp)
	}
	again, err := roundTripped.MarshalBinary()
	if err != nil {
		t.Fatalf("re-MarshalBinary: %v", err)
	}
	if !bytes.Equal(data, again) {
		t.Fatalf("re-encoding differs:\n got % x\nwant % x", again, data)
	}
}

// A caller that supplies sysUpTime.0 itself must not get a second one.
func TestNotifyDoesNotDuplicateSuppliedSysUpTime(t *testing.T) {
	n := &Notify{Timestamp: 5 * time.Second}
	n.Variables.Add(OIDSysUpTime, VariableTypeTimeTicks, 9*time.Second)
	n.Variables.Add(OIDSnmpTrapOID, VariableTypeObjectIdentifier, "1.3.6.1.4.1.42.0.1")

	data, err := n.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	var decoded Variables
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("decoded %d varbinds, want 2", len(decoded))
	}
	if decoded[0].Value != 9*time.Second {
		t.Fatalf("sysUpTime.0 = %v, want the caller's 9s", decoded[0].Value)
	}
}

// RFC 2741 6.2.10 requires snmpTrapOID.0. A master agent answers a
// notification without it with processingError and generates nothing, so the
// trap disappears; catching it here turns that into a visible error.
func TestNotifyRequiresSnmpTrapOID(t *testing.T) {
	cases := []struct {
		name string
		set  func(*Notify)
	}{
		{"no varbinds at all", func(n *Notify) {}},
		{"only sysUpTime.0", func(n *Notify) {
			n.Variables.Add(OIDSysUpTime, VariableTypeTimeTicks, time.Second)
		}},
		{"some other varbind first", func(n *Notify) {
			n.Variables.Add(value.OID{1, 3, 6, 1, 4, 1, 42}, VariableTypeInteger, int32(1))
		}},
		{"trap oid in the wrong position", func(n *Notify) {
			n.Variables.Add(value.OID{1, 3, 6, 1, 4, 1, 42}, VariableTypeInteger, int32(1))
			n.Variables.Add(OIDSnmpTrapOID, VariableTypeObjectIdentifier, "1.3.6.1.4.1.42.0.1")
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := &Notify{}
			c.set(n)
			if _, err := n.MarshalBinary(); err == nil {
				t.Fatal("MarshalBinary succeeded without snmpTrapOID.0 in the required position")
			}
		})
	}

	// The accepted shapes: trap OID first, or second behind sysUpTime.0.
	n := &Notify{}
	n.Variables.Add(OIDSnmpTrapOID, VariableTypeObjectIdentifier, "1.3.6.1.4.1.42.0.1")
	if _, err := n.MarshalBinary(); err != nil {
		t.Fatalf("MarshalBinary rejected a valid notification: %v", err)
	}
}

// RFC 2741 6.2.1 and 6.2.3 encode a timeout as one byte of seconds; 6.2.4
// leaves that byte reserved for the Unregister PDU. The Timeout type is shared
// by all three, so each has to place its fields itself.
func TestOpenGolden(t *testing.T) {
	o := &Open{}
	o.Timeout.Duration = 5 * time.Second
	o.Timeout.Priority = 127 // must not reach the wire: RFC 2741 6.2.1 reserves the byte
	o.ID.SetIdentifier(value.OID{1, 3, 6, 1})
	o.Description.Text = "test"

	data, err := o.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	want := []byte{5, 0, 0, 0}
	want = append(want, le32([]byte{4, 0, 0, 0}, 1, 3, 6, 1)...)
	want = append(want, append(le32(nil, 4), 't', 'e', 's', 't')...)
	if !bytes.Equal(data, want) {
		t.Fatalf("MarshalBinary =\n % x\nwant\n % x", data, want)
	}

	var decoded Open
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	if decoded.Timeout.Duration != 5*time.Second || decoded.Description.Text != "test" {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestRegisterGolden(t *testing.T) {
	r := &Register{}
	r.Timeout.Duration = 5 * time.Second
	r.Timeout.Priority = 127
	r.Subtree.SetIdentifier(value.OID{1, 3, 6, 1, 4, 1, 42})

	data, err := r.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	// r.timeout, r.priority, r.range_subid, <reserved>
	want := []byte{5, 127, 0, 0}
	want = append(want, le32([]byte{7, 0, 0, 0}, 1, 3, 6, 1, 4, 1, 42)...)
	if !bytes.Equal(data, want) {
		t.Fatalf("MarshalBinary =\n % x\nwant\n % x", data, want)
	}
}

// RFC 2741 6.2.4: the first payload byte of an Unregister is <reserved>, not a
// timeout. Writing a timeout there is a protocol violation a lenient master
// silently tolerates and a strict one rejects.
func TestUnregisterReservesTheTimeoutByte(t *testing.T) {
	u := &Unregister{}
	u.Timeout.Duration = 5 * time.Second
	u.Timeout.Priority = 127
	u.Subtree.SetIdentifier(value.OID{1, 3, 6, 1, 4, 1, 42})

	data, err := u.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	want := []byte{0, 127, 0, 0}
	want = append(want, le32([]byte{7, 0, 0, 0}, 1, 3, 6, 1, 4, 1, 42)...)
	if !bytes.Equal(data, want) {
		t.Fatalf("MarshalBinary =\n % x\nwant\n % x", data, want)
	}
}

// RFC 2741 6.2.1/6.2.3 give the timeout a single byte, so anything above 255
// seconds cannot be represented. Truncating it silently turns a 256 second
// timeout into a 0 second one, which means "use the master's default".
func TestTimeoutRejectsUnrepresentableDurations(t *testing.T) {
	for _, d := range []time.Duration{-time.Second, 256 * time.Second, time.Hour} {
		if _, err := (&Timeout{Duration: d}).MarshalBinary(); err == nil {
			t.Fatalf("MarshalBinary accepted %s, want an error", d)
		}
	}
	if _, err := (&Timeout{Duration: MaxTimeout}).MarshalBinary(); err != nil {
		t.Fatalf("MarshalBinary rejected the maximum of %s: %v", MaxTimeout, err)
	}
}

// RFC 2741 6.2.9: "These PDUs consist of the AgentX header only."
func TestSetPhasePDUsHaveNoPayload(t *testing.T) {
	packets := []Packet{&CommitSet{}, &UndoSet{}, &CleanupSet{}}

	for _, packet := range packets {
		data, err := packet.MarshalBinary()
		if err != nil {
			t.Fatalf("%T MarshalBinary: %v", packet, err)
		}
		if len(data) != 0 {
			t.Fatalf("%T encoded %d payload bytes, want 0", packet, len(data))
		}
		if err := packet.UnmarshalBinary(nil); err != nil {
			t.Fatalf("%T UnmarshalBinary(empty): %v", packet, err)
		}
		if err := packet.UnmarshalBinary([]byte{1, 2, 3, 4}); err == nil {
			t.Fatalf("%T accepted a non-empty payload", packet)
		}
	}
}

// TestGetCarriesASearchRangeList pins RFC 2741 6.2.5: a Get holds a
// SearchRangeList, and 7.2.3 requires a subagent to support several variables
// in one PDU.
func TestGetCarriesASearchRangeList(t *testing.T) {
	data := le32([]byte{1, 0, 1, 0}, 1) // start 1, include
	data = append(data, 0, 0, 0, 0)     // end: null OID
	data = append(data, le32([]byte{1, 0, 1, 0}, 2)...)
	data = append(data, 0, 0, 0, 0)

	var g Get
	if err := g.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	if len(g.SearchRanges) != 2 {
		t.Fatalf("decoded %d search ranges, want 2", len(g.SearchRanges))
	}
	if got := g.GetOID().String(); got != "1" {
		t.Fatalf("GetOID = %s, want 1", got)
	}
	if got := g.SearchRanges[1].From.GetIdentifier().String(); got != "2" {
		t.Fatalf("second search range = %s, want 2", got)
	}

	again, err := g.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	if !bytes.Equal(data, again) {
		t.Fatalf("re-encoding differs:\n got % x\nwant % x", again, data)
	}
}

// SetOID has to work on a zero-valued Get, which is how callers build one.
func TestGetSetOIDOnEmptyGet(t *testing.T) {
	g := &Get{}
	if g.GetOID() != nil {
		t.Fatal("GetOID on an empty Get is not nil")
	}
	g.SetOID(value.OID{1, 3, 6, 1})
	if got := g.GetOID().String(); got != "1.3.6.1" {
		t.Fatalf("GetOID = %s, want 1.3.6.1", got)
	}
}

func assertValueEqual(t *testing.T, got, want interface{}) {
	t.Helper()

	if wantBytes, ok := want.([]byte); ok {
		gotBytes, ok := got.([]byte)
		if !ok || !bytes.Equal(gotBytes, wantBytes) {
			t.Fatalf("Value = %v (%T), want %v", got, got, want)
		}
		return
	}
	if got != want {
		t.Fatalf("Value = %v (%T), want %v (%T)", got, got, want, want)
	}
}
