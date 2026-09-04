// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package agentx

import (
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/LJS360d/go-agentx/pdu"
)

// frameTimeout bounds every wait in these tests, so that a regression which
// stops the client answering shows up as a named failure instead of a hung
// test binary.
const frameTimeout = 2 * time.Second

// fakeMaster is an in-process stand-in for a master agent, speaking real
// AgentX over a net.Pipe. It exists so the client's protocol behaviour can be
// asserted byte for byte without an snmpd process: what the subagent must put
// on the wire in reply to a given PDU is exactly what RFC 2741 specifies, and
// that is testable in isolation.
//
// net.Pipe has no buffering at all - a Write blocks until the peer reads every
// byte - so the reads and writes each get their own goroutine. A master that
// read and wrote in lockstep would deadlock against the client's single
// dispatcher goroutine as soon as two requests were in flight; a real socket's
// send buffer hides that, a pipe does not.
type fakeMaster struct {
	conn net.Conn

	frames chan *pdu.HeaderPacket
	writes chan *pdu.HeaderPacket
	raw    chan []byte
	errs   chan error
}

// newPipeClient wires a Client to an in-memory net.Pipe instead of a real
// socket, so these tests exercise the real runReceiver/runDispatcher state
// machine without a snmpd master agent.
func newPipeClient(t *testing.T, opts ...DialOption) (*Client, *fakeMaster) {
	t.Helper()

	clientConn, masterConn := net.Pipe()
	opts = append([]DialOption{WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))}, opts...)
	c := newClient("pipe", "pipe", clientConn, opts...)

	m := &fakeMaster{
		conn:   masterConn,
		frames: make(chan *pdu.HeaderPacket, 64),
		writes: make(chan *pdu.HeaderPacket, 64),
		raw:    make(chan []byte, 64),
		errs:   make(chan error, 64),
	}

	go m.readLoop()
	go m.writeLoop()

	t.Cleanup(func() {
		c.Close()
		masterConn.Close()
	})

	return c, m
}

func (m *fakeMaster) readLoop() {
	for {
		hp, err := readFrameOrErr(m.conn)
		if err != nil {
			select {
			case m.errs <- err:
			default:
			}
			close(m.frames)
			return
		}
		m.frames <- hp
	}
}

func (m *fakeMaster) writeLoop() {
	for {
		select {
		case hp, ok := <-m.writes:
			if !ok {
				return
			}
			data, err := hp.MarshalBinary()
			if err != nil {
				m.errs <- err
				return
			}
			if _, err := m.conn.Write(data); err != nil {
				m.errs <- err
				return
			}
		case data := <-m.raw:
			if _, err := m.conn.Write(data); err != nil {
				m.errs <- err
				return
			}
		}
	}
}

// send queues a PDU for the subagent.
func (m *fakeMaster) send(hp *pdu.HeaderPacket) {
	m.writes <- hp
}

// sendRaw queues bytes verbatim, for the malformed frames a real peer can
// produce but this library's encoders cannot.
func (m *fakeMaster) sendRaw(data []byte) {
	m.raw <- data
}

// recv waits for the next PDU the subagent sends.
func (m *fakeMaster) recv(t *testing.T) *pdu.HeaderPacket {
	t.Helper()

	select {
	case hp, ok := <-m.frames:
		if !ok {
			t.Fatalf("connection closed while waiting for a frame: %v", m.lastError())
		}
		return hp
	case <-time.After(frameTimeout):
		t.Fatalf("timed out after %s waiting for a frame from the subagent", frameTimeout)
		return nil
	}
}

// expectSilence asserts the subagent sends nothing for the given period. This
// is how "no response is sent" (RFC 2741 7.2.4.4) is verified.
func (m *fakeMaster) expectSilence(t *testing.T, d time.Duration) {
	t.Helper()

	select {
	case hp, ok := <-m.frames:
		if ok {
			t.Fatalf("expected no reply, got %v", hp)
		}
	case <-time.After(d):
	}
}

func (m *fakeMaster) lastError() error {
	select {
	case err := <-m.errs:
		return err
	default:
		return nil
	}
}

// respond replies to req with an ordinary success response, echoing the
// identifiers RFC 2741 6.2.16 requires a response to carry back.
func (m *fakeMaster) respond(req *pdu.HeaderPacket, sessionID uint32, err pdu.Error) {
	m.send(&pdu.HeaderPacket{
		Header: &pdu.Header{
			Type:          pdu.TypeResponse,
			SessionID:     sessionID,
			TransactionID: req.Header.TransactionID,
			PacketID:      req.Header.PacketID,
		},
		Packet: &pdu.Response{Error: err},
	})
}

// serve answers the next n administrative requests (open, register, notify,
// close, ...) on a goroutine, assigning sessionID to any open. Session setup
// calls block until their response arrives, so the answering has to happen
// concurrently with them.
func (m *fakeMaster) serve(t *testing.T, n int, sessionID uint32) chan *pdu.HeaderPacket {
	t.Helper()

	served := make(chan *pdu.HeaderPacket, n)
	go func() {
		defer close(served)
		for i := 0; i < n; i++ {
			select {
			case req, ok := <-m.frames:
				if !ok {
					return
				}
				id := req.Header.SessionID
				if req.Header.Type == pdu.TypeOpen {
					id = sessionID
				}
				m.respond(req, id, pdu.ErrorNone)
				served <- req
			case <-time.After(frameTimeout):
				return
			}
		}
	}()
	return served
}

// openSession opens a session against the fake master and returns it.
func (m *fakeMaster) openSession(t *testing.T, c *Client, sessionID uint32, handler Handler) *Session {
	t.Helper()

	served := m.serve(t, 1, sessionID)

	session, err := c.Session(nil, "test", handler)
	if err != nil {
		t.Fatalf("Session: %v", err)
	}

	req, ok := <-served
	if !ok {
		t.Fatal("fake master did not see an open request")
	}
	if req.Header.Type != pdu.TypeOpen {
		t.Fatalf("first request was %v, want TypeOpen", req.Header.Type)
	}
	if session.ID() != sessionID {
		t.Fatalf("session ID = %d, want %d", session.ID(), sessionID)
	}
	return session
}

// readFrameOrErr reads one full AgentX PDU off conn using the same framing the
// client relies on (RFC 2741 6.1: fixed 20 byte header, then exactly
// PayloadLength bytes of payload). It reports failure via the returned error
// rather than t.Fatalf, so it is safe to call from a goroutine other than the
// one running the test.
func readFrameOrErr(conn net.Conn) (*pdu.HeaderPacket, error) {
	headerBytes := make([]byte, pdu.HeaderSize)
	if _, err := io.ReadFull(conn, headerBytes); err != nil {
		return nil, err
	}
	header := &pdu.Header{}
	if err := header.UnmarshalBinary(headerBytes); err != nil {
		return nil, err
	}

	payload := make([]byte, header.PayloadLength)
	if header.PayloadLength > 0 {
		if _, err := io.ReadFull(conn, payload); err != nil {
			return nil, err
		}
	}

	var packet pdu.Packet
	switch header.Type {
	case pdu.TypeResponse:
		packet = &pdu.Response{}
	case pdu.TypeOpen:
		packet = &pdu.Open{}
	case pdu.TypeClose:
		packet = &pdu.Close{}
	case pdu.TypeRegister:
		packet = &pdu.Register{}
	case pdu.TypeUnregister:
		packet = &pdu.Unregister{}
	case pdu.TypeNotify:
		packet = &pdu.Notify{}
	case pdu.TypeGet:
		packet = &pdu.Get{}
	case pdu.TypeGetNext:
		packet = &pdu.GetNext{}
	default:
		packet = &pdu.Unsupported{PacketType: header.Type}
	}
	if err := packet.UnmarshalBinary(payload); err != nil {
		return nil, err
	}

	return &pdu.HeaderPacket{Header: header, Packet: packet}, nil
}

// request builds a master-to-subagent request PDU.
func masterRequest(t pdu.Type, sessionID, transactionID, packetID uint32, packet pdu.Packet) *pdu.HeaderPacket {
	return &pdu.HeaderPacket{
		Header: &pdu.Header{
			Type:          t,
			SessionID:     sessionID,
			TransactionID: transactionID,
			PacketID:      packetID,
		},
		Packet: packet,
	}
}

// responseOf asserts a frame is a response and returns it.
func responseFrom(t *testing.T, hp *pdu.HeaderPacket) *pdu.Response {
	t.Helper()

	if hp.Header.Type != pdu.TypeResponse {
		t.Fatalf("frame type = %v, want TypeResponse", hp.Header.Type)
	}
	response, ok := hp.Packet.(*pdu.Response)
	if !ok {
		t.Fatalf("packet has type %T, want *pdu.Response", hp.Packet)
	}
	return response
}
