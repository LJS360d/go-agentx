# AgentX

[![Documentation](https://godoc.org/github.com/LJS360d/go-agentx?status.svg)](http://godoc.org/github.com/LJS360d/go-agentx)

A library with a pure Go implementation of the [AgentX-Protocol](http://tools.ietf.org/html/rfc2741) (RFC 2741). It implements the **subagent** side of the protocol; it is not a master agent.

The AgentX-Protocol can be used to extend a snmp-daemon such that it dispatches the requests to an OID-subtree to your Go application. Those requests are than handled by this library and can be replied with metrics about your applications state.

## State

The library implements all variable types (Integer, OctetString, Null, ObjectIdentifier, IPAddress, Counter32, Gauge32, TimeTicks, Opaque, Counter64, NoSuchObject, NoSuchInstance, EndOfMIBView) and the Get, GetNext, Set (TestSet/CommitSet/UndoSet/CleanupSet) and Notify/trap operations.

**GetBulk is not implemented**, and neither are non-default contexts, Ping, index allocation or agent capabilities. [`docs/RFC2741-COMPLIANCE.md`](docs/RFC2741-COMPLIANCE.md) is a section-by-section account of what is implemented, what is partial and what is missing. [`docs/REVIEW.md`](docs/REVIEW.md) records the conformance and robustness defects that were found and fixed, each with the test that pins it.

## Set operations

A `Handler` that implements only `agentx.Handler` applies a set during the **testSet** phase, before the master agent has decided to commit. Implement the optional `agentx.SetHandler` interface to get the two-phase behaviour RFC 2741 7.2.4 describes: `TestSet` validates, `CommitSet` applies, `UndoSet` rolls back.

## Tests

```sh
go test -race ./...                 # unit tests, an in-process fake master agent, fuzz targets
go test -tags snmpd ./...           # end-to-end against a real net-snmp snmpd (needs the binary)
go test -fuzz FuzzDecoders ./pdu    # everything a subagent parses comes off the network
```

## Helper

In order to provided metrics, your have to implement the `agentx.Handler` interface. For convenience, you can use the `agentx.ListHandler` implementation, which takes a list of OIDs and values and serves them if requested. An example is listed below.

## Example

```go
package main

import (
    "log"
    "net"
    "time"

    "github.com/LJS360d/go-agentx"
    "github.com/LJS360d/go-agentx/pdu"
    "github.com/LJS360d/go-agentx/value"
)

func main() {
    client, err := agentx.Dial("tcp", "localhost:705",
        agentx.WithTimeout(1 * time.Minute),
        agentx.WithReconnectInterval(1 * time.Second))
    if err != nil {
        log.Fatal(err)
    }

    listHandler := &agentx.ListHandler{}

    item := listHandler.Add("1.3.6.1.4.1.45995.3.1")
    item.Type = pdu.VariableTypeInteger
    item.Value = int32(-123)

    item = listHandler.Add("1.3.6.1.4.1.45995.3.2")
    item.Type = pdu.VariableTypeOctetString
    item.Value = "echo test"

    item = listHandler.Add("1.3.6.1.4.1.45995.3.3")
    item.Type = pdu.VariableTypeNull
    item.Value = nil

    item = listHandler.Add("1.3.6.1.4.1.45995.3.4")
    item.Type = pdu.VariableTypeObjectIdentifier
    item.Value = "1.3.6.1.4.1.45995.1.5"

    item = listHandler.Add("1.3.6.1.4.1.45995.3.5")
    item.Type = pdu.VariableTypeIPAddress
    item.Value = net.IP{10, 10, 10, 10}

    item = listHandler.Add("1.3.6.1.4.1.45995.3.6")
    item.Type = pdu.VariableTypeCounter32
    item.Value = uint32(123)

    item = listHandler.Add("1.3.6.1.4.1.45995.3.7")
    item.Type = pdu.VariableTypeGauge32
    item.Value = uint32(123)

    item = listHandler.Add("1.3.6.1.4.1.45995.3.8")
    item.Type = pdu.VariableTypeTimeTicks
    item.Value = 123 * time.Second

    item = listHandler.Add("1.3.6.1.4.1.45995.3.9")
    item.Type = pdu.VariableTypeOpaque
    item.Value = []byte{1, 2, 3}

    item = listHandler.Add("1.3.6.1.4.1.45995.3.10")
    item.Type = pdu.VariableTypeCounter64
    item.Value = uint64(12345678901234567890)

    session, err := client.Session(value.MustParseOID("1.3.6.1.4.1.45995"), "test client", listHandler)
    if err != nil {
        log.Fatal(err)
    }

    if err := session.Register(127, value.MustParseOID("1.3.6.1.4.1.45995.3")); err != nil {
        log.Fatal(err)
    }

    for {
        time.Sleep(100 * time.Millisecond)
    }
}
```

## Connection lost

If the connection to the snmp-daemon is lost, the client tries to reconnect. Therefor the property `ReconnectInterval` has be set. It specifies a duration that is waited before a re-connect is tried.
If the client has open session or registrations, the client try to re-establish both on a successful re-connect.

## Project

The implementation was provided by [simia.tech (haftungsbeschränkt)](https://simia.tech).

## License

The project is licensed under LGPL 3.0 (see LICENSE file).
