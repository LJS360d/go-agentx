// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package pdu

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/LJS360d/go-agentx/value"
)

// The two object identifiers RFC 2741 6.2.10 and 7.1.10 give a special meaning
// inside an agentx-Notify-PDU's VarBindList.
var (
	// OIDSysUpTime is sysUpTime.0. If the subagent supplies it, it must be the
	// first varbind.
	OIDSysUpTime = value.OID{1, 3, 6, 1, 2, 1, 1, 3, 0}
	// OIDSnmpTrapOID is snmpTrapOID.0. It must always be present: second if
	// sysUpTime.0 was supplied, first if it was not.
	OIDSnmpTrapOID = value.OID{1, 3, 6, 1, 6, 3, 1, 1, 4, 1, 0}
)

// Notify defines the pdu notify packet (used for traps).
//
// RFC 2741 6.2.10: the PDU is an optional context followed by a VarBindList
// and nothing else - there is no timestamp field on the wire. A timestamp is
// conveyed as a leading sysUpTime.0 varbind, which is what Timestamp is
// encoded as.
type Notify struct {
	// Timestamp, when non-zero, is emitted as a leading sysUpTime.0 TimeTicks
	// varbind. It is ignored if the caller already supplied sysUpTime.0 as the
	// first entry of Variables.
	Timestamp time.Duration
	Variables Variables
}

// Type returns the pdu packet type.
func (n *Notify) Type() Type {
	return TypeNotify
}

// varBindList returns the VarBindList as it goes on the wire, with the
// sysUpTime.0 binding materialised from Timestamp when needed.
func (n *Notify) varBindList() Variables {
	if n.Timestamp == 0 || n.hasSysUpTime() {
		return n.Variables
	}

	variables := make(Variables, 0, len(n.Variables)+1)
	variables.Add(OIDSysUpTime, VariableTypeTimeTicks, n.Timestamp)
	return append(variables, n.Variables...)
}

func (n *Notify) hasSysUpTime() bool {
	return len(n.Variables) > 0 && value.CompareOIDs(n.Variables[0].Name.GetIdentifier(), OIDSysUpTime) == 0
}

// MarshalBinary returns the pdu packet as a slice of bytes.
func (n *Notify) MarshalBinary() ([]byte, error) {
	variables := n.varBindList()
	if err := validateNotifyVariables(variables); err != nil {
		return nil, err
	}
	return variables.MarshalBinary()
}

// validateNotifyVariables enforces the two restrictions RFC 2741 6.2.10 places
// on a Notify VarBindList. A master agent rejects a notification that breaks
// them with processingError, so catching it here turns a silently dropped trap
// into an error the caller can see.
func validateNotifyVariables(variables Variables) error {
	trapOIDIndex := 0
	if len(variables) > 0 && value.CompareOIDs(variables[0].Name.GetIdentifier(), OIDSysUpTime) == 0 {
		trapOIDIndex = 1
	}

	if len(variables) <= trapOIDIndex ||
		value.CompareOIDs(variables[trapOIDIndex].Name.GetIdentifier(), OIDSnmpTrapOID) != 0 {
		return fmt.Errorf("notify: snmpTrapOID.0 (%s) must be present as varbind %d (RFC 2741 6.2.10)",
			OIDSnmpTrapOID, trapOIDIndex+1)
	}

	return nil
}

// UnmarshalBinary sets the packet structure from the provided slice of bytes.
func (n *Notify) UnmarshalBinary(data []byte) error {
	return n.UnmarshalBinaryOrder(data, binary.LittleEndian)
}

// UnmarshalBinaryOrder sets the packet structure from the provided slice of
// bytes, decoding multi-byte fields in the byte order the header declared.
func (n *Notify) UnmarshalBinaryOrder(data []byte, order binary.ByteOrder) error {
	if err := n.Variables.UnmarshalBinaryOrder(data, order); err != nil {
		return err
	}

	n.Timestamp = 0
	if n.hasSysUpTime() {
		if timestamp, ok := n.Variables[0].Value.(time.Duration); ok {
			n.Timestamp = timestamp
		}
	}

	return nil
}

func (n *Notify) String() string {
	return "(notify " + n.Variables.String() + ")"
}
