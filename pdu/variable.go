// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/LJS360d/go-agentx/value"
)

// variableHeaderSize is the fixed part of a VarBind that precedes v.name: a
// 2-byte type and 2 reserved bytes (RFC 2741 5.4).
const variableHeaderSize = 4

// Variable defines the pdu varbind packet.
type Variable struct {
	Type  VariableType
	Name  ObjectIdentifier
	Value interface{}
}

// Set sets the variable.
func (v *Variable) Set(oid value.OID, t VariableType, value interface{}) {
	v.Name.SetIdentifier(oid)
	v.Type = t
	v.Value = value
}

// ByteSize returns the number of bytes, the binding would need in the encoded version.
//
// A variable whose Value does not match its Type cannot be encoded at all; in
// that case the size of the fixed part is reported. Decoding never relies on
// this - Variables tracks the bytes each VarBind actually consumed - so an
// unencodable variable cannot desynchronise a parse.
func (v *Variable) ByteSize() int {
	data, err := v.MarshalBinary()
	if err != nil {
		return variableHeaderSize + v.Name.ByteSize()
	}
	return len(data)
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (v *Variable) MarshalBinary() ([]byte, error) {
	buffer := &bytes.Buffer{}

	binary.Write(buffer, binary.LittleEndian, &v.Type)
	buffer.WriteByte(0x00)
	buffer.WriteByte(0x00)

	nameBytes, err := v.Name.MarshalBinary()
	if err != nil {
		return nil, err
	}
	buffer.Write(nameBytes)

	valueBytes, err := v.marshalValue()
	if err != nil {
		return nil, err
	}
	buffer.Write(valueBytes)

	return buffer.Bytes(), nil
}

// marshalValue encodes v.data (RFC 2741 5.4). Every type mismatch is reported
// as an error: a variable binding is frequently built from values a Handler
// implementation supplies, and a wrong Go type there must not take the process
// down.
func (v *Variable) marshalValue() ([]byte, error) {
	switch v.Type {
	case VariableTypeNull, VariableTypeNoSuchObject, VariableTypeNoSuchInstance, VariableTypeEndOfMIBView:
		// RFC 2741 5.4: "Value data never follows v.name in these cases."
		return nil, nil

	case VariableTypeInteger:
		i, ok := v.Value.(int32)
		if !ok {
			return nil, typeError(v, "int32")
		}
		return binary.LittleEndian.AppendUint32(nil, uint32(i)), nil

	case VariableTypeCounter32, VariableTypeGauge32:
		u, ok := v.Value.(uint32)
		if !ok {
			return nil, typeError(v, "uint32")
		}
		return binary.LittleEndian.AppendUint32(nil, u), nil

	case VariableTypeCounter64:
		u, ok := v.Value.(uint64)
		if !ok {
			return nil, typeError(v, "uint64")
		}
		return binary.LittleEndian.AppendUint64(nil, u), nil

	case VariableTypeTimeTicks:
		// TimeTicks counts hundredths of a second and wraps by definition, so
		// the truncation to 32 bits is the intended behaviour rather than an
		// overflow to reject.
		var ticks uint32
		switch val := v.Value.(type) {
		case time.Duration:
			if val < 0 {
				return nil, fmt.Errorf("variable %s: negative duration %s", v.Name, val)
			}
			ticks = uint32(val / (10 * time.Millisecond))
		case uint32:
			ticks = val
		default:
			return nil, typeError(v, "time.Duration or uint32")
		}
		return binary.LittleEndian.AppendUint32(nil, ticks), nil

	case VariableTypeOctetString, VariableTypeOpaque:
		text, err := textValue(v)
		if err != nil {
			return nil, err
		}
		return (&OctetString{Text: text}).MarshalBinary()

	case VariableTypeIPAddress:
		// RFC 2741 5.4: an IpAddress is an Octet String whose octets are
		// ordered most significant first. net.ParseIP yields a 16-byte
		// IPv4-in-IPv6 representation for a v4 address, which would put 16
		// bytes on the wire where a manager expects 4.
		var ip net.IP
		switch val := v.Value.(type) {
		case net.IP:
			ip = val
		case []byte:
			ip = net.IP(val)
		case string:
			ip = net.IP(val)
		default:
			return nil, typeError(v, "net.IP")
		}
		v4 := ip.To4()
		if v4 == nil {
			return nil, fmt.Errorf("variable %s: %v is not an IPv4 address; SNMP IpAddress is 4 octets", v.Name, ip)
		}
		return (&OctetString{Text: string(v4)}).MarshalBinary()

	case VariableTypeObjectIdentifier:
		oid, err := oidValue(v)
		if err != nil {
			return nil, err
		}
		oi := &ObjectIdentifier{}
		oi.SetIdentifier(oid)
		return oi.MarshalBinary()
	}

	return nil, fmt.Errorf("unhandled variable type %s", v.Type)
}

func textValue(v *Variable) (string, error) {
	switch val := v.Value.(type) {
	case string:
		return val, nil
	case []byte:
		return string(val), nil
	}
	return "", typeError(v, "string or []byte")
}

func oidValue(v *Variable) (value.OID, error) {
	switch val := v.Value.(type) {
	case value.OID:
		return val, nil
	case []uint32:
		return value.OID(val), nil
	case string:
		return value.ParseOID(val)
	}
	return nil, typeError(v, "value.OID or string")
}

func typeError(v *Variable, want string) error {
	return fmt.Errorf("variable %s: %s needs a %s value, got %T", v.Name, v.Type, want, v.Value)
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
func (v *Variable) UnmarshalBinary(data []byte) error {
	return v.UnmarshalBinaryOrder(data, binary.LittleEndian)
}

// UnmarshalBinaryOrder sets the packet structure from the provided slice of
// bytes, decoding multi-byte fields in the byte order the enclosing PDU header
// declared (RFC 2741 5).
func (v *Variable) UnmarshalBinaryOrder(data []byte, order binary.ByteOrder) error {
	_, err := v.unmarshal(data, order)
	return err
}

// unmarshal decodes one VarBind and reports how many bytes it consumed. The
// consumed count is what a VarBindList uses to find the next binding; deriving
// it by re-encoding the decoded value instead would fail for any value this
// library can decode but not encode.
func (v *Variable) unmarshal(data []byte, order binary.ByteOrder) (int, error) {
	if len(data) < variableHeaderSize {
		return 0, fmt.Errorf("variable: short buffer: got %d bytes, want at least %d", len(data), variableHeaderSize)
	}

	v.Type = VariableType(order.Uint16(data))

	rest := data[variableHeaderSize:]
	if err := v.Name.UnmarshalBinaryOrder(rest, order); err != nil {
		return 0, err
	}
	nameSize := v.Name.ByteSize()
	rest = rest[nameSize:]

	valueSize, err := v.unmarshalValue(rest, order)
	if err != nil {
		return 0, err
	}

	return variableHeaderSize + nameSize + valueSize, nil
}

func (v *Variable) unmarshalValue(data []byte, order binary.ByteOrder) (int, error) {
	fixed := func(size int) error {
		if len(data) < size {
			return fmt.Errorf("variable %s: short buffer: got %d bytes, want %d", v.Type, len(data), size)
		}
		return nil
	}

	switch v.Type {
	case VariableTypeNull, VariableTypeNoSuchObject, VariableTypeNoSuchInstance, VariableTypeEndOfMIBView:
		v.Value = nil
		return 0, nil

	case VariableTypeInteger:
		if err := fixed(4); err != nil {
			return 0, err
		}
		v.Value = int32(order.Uint32(data))
		return 4, nil

	case VariableTypeCounter32, VariableTypeGauge32:
		if err := fixed(4); err != nil {
			return 0, err
		}
		v.Value = order.Uint32(data)
		return 4, nil

	case VariableTypeTimeTicks:
		if err := fixed(4); err != nil {
			return 0, err
		}
		v.Value = time.Duration(order.Uint32(data)) * 10 * time.Millisecond
		return 4, nil

	case VariableTypeCounter64:
		if err := fixed(8); err != nil {
			return 0, err
		}
		v.Value = order.Uint64(data)
		return 8, nil

	case VariableTypeOctetString, VariableTypeOpaque, VariableTypeIPAddress:
		octetString := &OctetString{}
		if err := octetString.UnmarshalBinaryOrder(data, order); err != nil {
			return 0, err
		}
		switch v.Type {
		case VariableTypeOctetString:
			v.Value = octetString.Text
		case VariableTypeOpaque:
			v.Value = []byte(octetString.Text)
		default:
			v.Value = net.IP(octetString.Text)
		}
		return octetString.ByteSize(), nil

	case VariableTypeObjectIdentifier:
		oid := &ObjectIdentifier{}
		if err := oid.UnmarshalBinaryOrder(data, order); err != nil {
			return 0, err
		}
		v.Value = oid.GetIdentifier()
		return oid.ByteSize(), nil
	}

	return 0, fmt.Errorf("unhandled variable type %s", v.Type)
}

func (v *Variable) String() string {
	return fmt.Sprintf("(variable %s = %v)", v.Type, v.Value)
}
