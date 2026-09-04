// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

//go:build snmpd

// These tests drive a real net-snmp master agent (snmpd) over a local socket.
// They are the end-to-end check that what this library puts on the wire is what
// an actual master agent accepts, and they need the snmpd binary and
// snmpd.conf, so they are kept out of the default `go test ./...` run:
//
//	go test -tags snmpd ./...

package agentx_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LJS360d/go-agentx"
	"github.com/LJS360d/go-agentx/pdu"
	"github.com/LJS360d/go-agentx/value"
)

func TestListHandler(t *testing.T) {
	e := setUpTestEnvironment(t)
	defer e.tearDown()

	lh := &agentx.ListHandler{}
	i := lh.Add("1.3.6.1.4.1.45995.3.1")
	i.Type = pdu.VariableTypeOctetString
	i.Value = "test"

	i = lh.Add("1.3.6.1.4.1.45995.3.3")
	i.Type = pdu.VariableTypeInteger
	i.Value = int32(-123)

	i = lh.Add("1.3.6.1.4.1.45995.3.4")
	i.Type = pdu.VariableTypeCounter32
	i.Value = uint32(123)

	session, err := e.client.Session(value.MustParseOID("1.3.6.1.4.1.45995"), "test client", lh)
	require.NoError(t, err)
	defer session.Close()

	baseOID := value.MustParseOID("1.3.6.1.4.1.45995")

	require.NoError(t, session.Register(127, baseOID))
	defer session.Unregister(127, baseOID)

	t.Run("Get", func(t *testing.T) {
		assert.Equal(t,
			".1.3.6.1.4.1.45995.3.1 = STRING: \"test\"",
			SNMPGet(t, "1.3.6.1.4.1.45995.3.1"))

		assert.Equal(t,
			".1.3.6.1.4.1.45995.3.2 = No Such Object available on this agent at this OID",
			SNMPGet(t, "1.3.6.1.4.1.45995.3.2"))

		// RFC 2741 6.2.5/7.2.3: one agentx-Get-PDU carries a SearchRangeList,
		// and the subagent must answer every entry. A subagent that reads only
		// the first search range answers this with a single varbind and the
		// master agent reports the rest as missing.
		assert.Equal(t,
			".1.3.6.1.4.1.45995.3.1 = STRING: \"test\"\n"+
				".1.3.6.1.4.1.45995.3.2 = No Such Object available on this agent at this OID\n"+
				".1.3.6.1.4.1.45995.3.3 = INTEGER: -123\n"+
				".1.3.6.1.4.1.45995.3.4 = Counter32: 123",
			SNMPGet(t,
				"1.3.6.1.4.1.45995.3.1",
				"1.3.6.1.4.1.45995.3.2",
				"1.3.6.1.4.1.45995.3.3",
				"1.3.6.1.4.1.45995.3.4"))
	})

	t.Run("GetNext", func(t *testing.T) {
		assert.Equal(t,
			".1.3.6.1.4.1.45995.3.1 = STRING: \"test\"",
			SNMPGetNext(t, "1.3.6.1.4.1.45995.3.0"))

		assert.Equal(t,
			".1.3.6.1.4.1.45995.3.1 = STRING: \"test\"",
			SNMPGetNext(t, "1.3.6.1.4.1.45995.3"))

	})

	t.Run("GetBulk", func(t *testing.T) {
		assert.Equal(t,
			".1.3.6.1.4.1.45995.3.1 = STRING: \"test\"",
			SNMPGetBulk(t, "1.3.6.1.4.1.45995.3.0", 0, 1))
	})
}
