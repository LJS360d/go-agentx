// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package agentx

import (
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/LJS360d/go-agentx/pdu"
	"github.com/LJS360d/go-agentx/value"
)

// A master agent that sends a PDU type this library does not implement
// (GetBulk, Ping, ...) must get an error response, not kill the receiver.
// RFC 2741 6.2.16 limits the error of a response to an "SNMP request
// processing" PDU (types 5-11, which GetBulk is) to the SNMPv2 error-status
// values, so genErr is the one to use here.
func TestUnsupportedPacketTypeIsAnsweredWithGenErr(t *testing.T) {
	c, m := newPipeClient(t)
	session := m.openSession(t, c, 7, nil)

	m.send(masterRequest(pdu.TypeGetBulk, session.ID(), 99, 1, &pdu.Unsupported{PacketType: pdu.TypeGetBulk}))

	response := responseFrom(t, m.recv(t))
	if response.Error != pdu.ErrorGenErr {
		t.Fatalf("response error = %v, want ErrorGenErr", response.Error)
	}

	// The receiver must still be alive: an ordinary request on the same
	// session still has to be answered.
	m.send(masterRequest(pdu.TypeGet, session.ID(), 100, 2, getOf(value.OID{1, 3, 6, 1})))
	if got := m.recv(t).Header.TransactionID; got != 100 {
		t.Fatalf("receiver did not survive the unsupported packet: transaction id %d, want 100", got)
	}
}

// runReceiver must keep one bufio.Reader for the life of a connection.
// Rebuilding it every loop iteration silently drops whatever it had already
// read ahead from the socket, so two PDUs delivered in a single underlying
// Read (as TCP/pipe coalescing routinely does) desync the frame boundary and
// the second one is lost. Writing both frames in one Write reproduces exactly
// that coalescing.
func TestReceiverHandlesPipelinedFrames(t *testing.T) {
	c, m := newPipeClient(t)
	session := m.openSession(t, c, 1, nil)

	var combined []byte
	for _, transactionID := range []uint32{1, 2} {
		hp := masterRequest(pdu.TypeGet, session.ID(), transactionID, transactionID, getOf(value.OID{1, 3, 6, 1}))
		data, err := hp.MarshalBinary()
		if err != nil {
			t.Fatalf("marshal frame %d: %v", transactionID, err)
		}
		combined = append(combined, data...)
	}
	m.sendRaw(combined)

	if got := m.recv(t).Header.TransactionID; got != 1 {
		t.Fatalf("first response transaction id = %d, want 1", got)
	}
	if got := m.recv(t).Header.TransactionID; got != 2 {
		t.Fatalf("second response transaction id = %d, want 2 (frame was dropped or misparsed)", got)
	}
}

// RFC 2741 7.2.2: "if h.sessionID does not correspond to a currently
// established session, res.error is set to `notOpen'". Silently logging and
// dropping the PDU instead leaves the master agent waiting for its timeout.
func TestUnknownSessionIsAnsweredWithNotOpen(t *testing.T) {
	c, m := newPipeClient(t)
	m.openSession(t, c, 1, nil)

	m.send(masterRequest(pdu.TypeGet, 999, 5, 6, getOf(value.OID{1, 3, 6, 1})))

	frame := m.recv(t)
	response := responseFrom(t, frame)
	if response.Error != pdu.ErrorNotOpen {
		t.Fatalf("response error = %v, want ErrorNotOpen", response.Error)
	}
	if frame.Header.SessionID != 999 || frame.Header.PacketID != 6 {
		t.Fatalf("response addressed to session %d packet %d, want 999/6",
			frame.Header.SessionID, frame.Header.PacketID)
	}
}

// RFC 2741 7.2.2: "If the received PDU cannot be parsed, res.error is set to
// `parseError'". The old receiver returned from its goroutine on any decode
// error, which left the subagent silently deaf for the rest of the process's
// life - a single malformed PDU was a permanent denial of service.
func TestMalformedPayloadIsAnsweredWithParseErrorAndClientSurvives(t *testing.T) {
	c, m := newPipeClient(t)
	session := m.openSession(t, c, 3, nil)

	// A Get whose payload claims an object identifier with 100
	// sub-identifiers but carries none of them.
	payload := []byte{100, 0, 0, 0}
	header := &pdu.Header{
		Version:       pdu.Version,
		Type:          pdu.TypeGet,
		SessionID:     session.ID(),
		TransactionID: 11,
		PacketID:      12,
		PayloadLength: uint32(len(payload)),
	}
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	m.sendRaw(append(headerBytes, payload...))

	response := responseFrom(t, m.recv(t))
	if response.Error != pdu.ErrorParse {
		t.Fatalf("response error = %v, want ErrorParse", response.Error)
	}

	// The connection must still be usable.
	m.send(masterRequest(pdu.TypeGet, session.ID(), 13, 14, getOf(value.OID{1, 3, 6, 1})))
	if got := m.recv(t).Header.TransactionID; got != 13 {
		t.Fatalf("client did not survive a malformed payload: transaction id %d, want 13", got)
	}
}

// A header this library will not act on still has to leave the stream framed:
// its payload is skipped and a parseError is returned, rather than the
// connection being abandoned.
func TestUnsupportedVersionIsAnsweredWithParseError(t *testing.T) {
	c, m := newPipeClient(t)
	session := m.openSession(t, c, 3, nil)

	header := &pdu.Header{Version: 2, Type: pdu.TypeGet, SessionID: session.ID(), TransactionID: 21, PacketID: 22, PayloadLength: 8}
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	m.sendRaw(append(headerBytes, make([]byte, 8)...))

	response := responseFrom(t, m.recv(t))
	if response.Error != pdu.ErrorParse {
		t.Fatalf("response error = %v, want ErrorParse", response.Error)
	}

	m.send(masterRequest(pdu.TypeGet, session.ID(), 23, 24, getOf(value.OID{1, 3, 6, 1})))
	if got := m.recv(t).Header.TransactionID; got != 23 {
		t.Fatalf("client lost frame sync after a rejected header: transaction id %d, want 23", got)
	}
}

// RFC 2741 5 and 6.1: the NETWORK_BYTE_ORDER flag is chosen per PDU by its
// sender and governs every multi-byte field in it, payload included. A master
// agent on a big-endian host sets it; decoding its search ranges as
// little-endian yields byte-swapped OIDs.
func TestBigEndianRequestIsDecoded(t *testing.T) {
	c, m := newPipeClient(t)

	want := value.OID{1, 3, 6, 1, 4, 1, 42}
	handler := &testHandler{
		get: func(oid value.OID) (value.OID, pdu.VariableType, any, error) {
			if oid.String() != want.String() {
				return nil, 0, nil, errors.New("wrong oid: " + oid.String())
			}
			return oid, pdu.VariableTypeInteger, int32(1), nil
		},
	}
	session := m.openSession(t, c, 4, handler)

	// Hand-encode the search range list in network byte order.
	payload := []byte{byte(len(want)), 0, 1, 0}
	for _, sub := range want {
		payload = binary.BigEndian.AppendUint32(payload, sub)
	}
	payload = append(payload, 0, 0, 0, 0) // null ending OID

	header := &pdu.Header{
		Version:       pdu.Version,
		Type:          pdu.TypeGet,
		Flags:         pdu.FlagNetworkByteOrder,
		SessionID:     session.ID(),
		TransactionID: 31,
		PacketID:      32,
		PayloadLength: uint32(len(payload)),
	}
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	m.sendRaw(append(headerBytes, payload...))

	response := responseFrom(t, m.recv(t))
	if response.Error != pdu.ErrorNone {
		t.Fatalf("response error = %v, want ErrorNone (the big-endian OID was misdecoded)", response.Error)
	}
	if len(response.Variables) != 1 {
		t.Fatalf("got %d varbinds, want 1", len(response.Variables))
	}
	if got := response.Variables[0].Name.GetIdentifier().String(); got != want.String() {
		t.Fatalf("responded about %s, want %s", got, want)
	}
}

// A header may announce any payload length at all, and the receiver allocates
// that many bytes before reading one of them. Without a bound, a single
// 20-byte header is an out-of-memory.
func TestImplausiblePayloadLengthDoesNotAllocate(t *testing.T) {
	c, m := newPipeClient(t)
	session := m.openSession(t, c, 5, nil)

	headerBytes := make([]byte, pdu.HeaderSize)
	headerBytes[0] = pdu.Version
	headerBytes[1] = byte(pdu.TypeGet)
	binary.LittleEndian.PutUint32(headerBytes[4:], session.ID())
	binary.LittleEndian.PutUint32(headerBytes[16:], 0xFFFFFFF0) // ~4 GiB, 4-byte aligned
	m.sendRaw(headerBytes)

	// The client must give up on the connection rather than try to allocate
	// it. Either it reports the error and stops, or it closes; what it must
	// not do is answer as if the PDU were fine.
	m.expectSilence(t, 200*time.Millisecond)

	select {
	case err := <-c.Error():
		if err == nil {
			t.Fatal("client reported a nil error")
		}
	case <-time.After(frameTimeout):
		t.Fatal("client did not report an error for an implausible payload length")
	}
}

// A request whose response never arrives must not park the caller forever.
func TestRequestTimesOut(t *testing.T) {
	c, m := newPipeClient(t, WithTimeout(150*time.Millisecond))

	// Nothing answers, so the open request must come back as a timeout.
	done := make(chan error, 1)
	go func() {
		_, err := c.Session(nil, "test", nil)
		done <- err
	}()

	// Drain the request so the write does not block forever on the pipe.
	m.recv(t)

	select {
	case err := <-done:
		if !errors.Is(err, ErrRequestTimeout) {
			t.Fatalf("Session error = %v, want ErrRequestTimeout", err)
		}
	case <-time.After(frameTimeout):
		t.Fatal("Session did not time out")
	}
}

// A late response for a request that already timed out must be dropped, not
// delivered to the next caller and not left leaking in the pending map.
func TestLateResponseIsDiscarded(t *testing.T) {
	c, m := newPipeClient(t, WithTimeout(150*time.Millisecond))

	done := make(chan error, 1)
	go func() {
		_, err := c.Session(nil, "first", nil)
		done <- err
	}()
	first := m.recv(t)

	if err := <-done; !errors.Is(err, ErrRequestTimeout) {
		t.Fatalf("first Session error = %v, want ErrRequestTimeout", err)
	}

	// Answer the abandoned request, then make a fresh one.
	m.respond(first, 1, pdu.ErrorNone)

	served := m.serve(t, 1, 42)
	session, err := c.Session(nil, "second", nil)
	if err != nil {
		t.Fatalf("second Session: %v", err)
	}
	<-served
	if session.ID() != 42 {
		t.Fatalf("session ID = %d, want 42; the stale response was delivered to the wrong request", session.ID())
	}
}

// c.sessions is written from Session() and read/written from the receiver and
// dispatcher goroutines; run with -race to prove sessionsMu guards every
// access.
func TestConcurrentSessionsDoNotRace(t *testing.T) {
	c, m := newPipeClient(t)

	const n = 20

	go func() {
		var sessionID uint32
		for i := 0; i < n; i++ {
			req, ok := <-m.frames
			if !ok {
				return
			}
			sessionID++
			m.respond(req, sessionID, pdu.ErrorNone)
		}
	}()

	var wg sync.WaitGroup
	seen := make(chan uint32, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			session, err := c.Session(nil, "test", nil)
			if err != nil {
				t.Errorf("Session: %v", err)
				return
			}
			seen <- session.ID()
		}()
	}
	wg.Wait()
	close(seen)

	ids := make(map[uint32]bool)
	for id := range seen {
		ids[id] = true
	}
	if len(ids) != n {
		t.Fatalf("got %d distinct session IDs, want %d", len(ids), n)
	}

	c.sessionsMu.RLock()
	stored := len(c.sessions)
	c.sessionsMu.RUnlock()
	if stored != n {
		t.Fatalf("c.sessions has %d entries, want %d", stored, n)
	}
}

// Close used to close errorChan while background goroutines were still free to
// send on it: reportError checked the closed flag, released the mutex, and
// only then sent. Anything landing in that window panicked with "send on
// closed channel".
func TestCloseDoesNotRaceErrorReports(t *testing.T) {
	c, _ := newPipeClient(t)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				c.reportError(errors.New("background"))
			}
		}()
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	wg.Wait()

	// Close is idempotent.
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// Requests made after Close must fail rather than block forever.
func TestRequestAfterCloseFails(t *testing.T) {
	c, _ := newPipeClient(t)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := c.Session(nil, "test", nil); !errors.Is(err, ErrClientClosed) {
		t.Fatalf("Session error = %v, want ErrClientClosed", err)
	}
}

// getOf builds a Get PDU for a single OID, the way a master agent would.
func getOf(oids ...value.OID) *pdu.Get {
	g := &pdu.Get{}
	for _, oid := range oids {
		r := pdu.Range{}
		r.From.SetIdentifier(oid)
		r.From.SetInclude(true)
		g.SearchRanges = append(g.SearchRanges, r)
	}
	return g
}

// getNextOf builds a GetNext PDU covering [from, to) for each pair.
func getNextOf(pairs ...[2]value.OID) *pdu.GetNext {
	g := &pdu.GetNext{}
	for _, pair := range pairs {
		r := pdu.Range{}
		r.From.SetIdentifier(pair[0])
		r.To.SetIdentifier(pair[1])
		g.SearchRanges = append(g.SearchRanges, r)
	}
	return g
}

// RFC 2741 7.1.8/7.1.9: a master agent forgets everything about a session when
// the connection drops - "if the subagent wishes to re-establish the session
// after it has been closed, it needs to re-register the MIB regions". So a
// reconnect has to replay the open and every registration, not just re-dial.
//
// This one needs a real listener rather than a pipe: reconnecting means
// re-dialling the address.
func TestReconnectReopensAndReregistersSessions(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	// served reports the PDU types each connection saw, so the test can assert
	// that the second connection replays the whole handshake.
	served := make(chan pdu.Type, 16)
	conns := make(chan net.Conn, 4)

	go func() {
		sessionID := uint32(0)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conns <- conn

			go func(conn net.Conn) {
				for {
					req, err := readFrameOrErr(conn)
					if err != nil {
						return
					}
					id := req.Header.SessionID
					if req.Header.Type == pdu.TypeOpen {
						sessionID++
						id = sessionID
					}
					response := &pdu.HeaderPacket{
						Header: &pdu.Header{
							Type:          pdu.TypeResponse,
							SessionID:     id,
							TransactionID: req.Header.TransactionID,
							PacketID:      req.Header.PacketID,
						},
						Packet: &pdu.Response{},
					}
					data, err := response.MarshalBinary()
					if err != nil {
						return
					}
					if _, err := conn.Write(data); err != nil {
						return
					}
					served <- req.Header.Type
				}
			}(conn)
		}
	}()

	c, err := Dial("tcp", listener.Addr().String(),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithTimeout(2*time.Second),
		WithReconnectInterval(10*time.Millisecond))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	session, err := c.Session(nil, "test", nil)
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	if got := <-served; got != pdu.TypeOpen {
		t.Fatalf("first PDU was %v, want TypeOpen", got)
	}

	if err := session.Register(127, value.MustParseOID("1.3.6.1.4.1.45995")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got := <-served; got != pdu.TypeRegister {
		t.Fatalf("second PDU was %v, want TypeRegister", got)
	}
	firstID := session.ID()

	// Drop the connection under the client.
	(<-conns).Close()

	for _, want := range []pdu.Type{pdu.TypeOpen, pdu.TypeRegister} {
		select {
		case got := <-served:
			if got != want {
				t.Fatalf("after reconnect the master saw %v, want %v", got, want)
			}
		case <-time.After(frameTimeout):
			t.Fatalf("timed out waiting for %v after the reconnect", want)
		}
	}

	// The session must be re-registered under the id the master just assigned,
	// otherwise its traffic is answered with notOpen.
	deadline := time.Now().Add(frameTimeout)
	for {
		if id := session.ID(); id != firstID {
			if _, ok := c.session(id); ok {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("session %d was not re-registered with its new id", session.ID())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// One session failing to re-open must not strand the others. Abandoning the
// loop on the first error used to leave every session after it removed from
// the routing table and never re-registered - permanently unreachable, with
// its handler still believing it was live.
func TestReconnectContinuesAfterOneSessionFails(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	var (
		mutex     sync.Mutex
		opens     int
		sessionID uint32
	)
	// failOpen is the ordinal of the re-open that gets rejected.
	const failOpen = 3

	conns := make(chan net.Conn, 4)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conns <- conn
			go func(conn net.Conn) {
				for {
					req, err := readFrameOrErr(conn)
					if err != nil {
						return
					}

					responseError := pdu.ErrorNone
					id := req.Header.SessionID
					if req.Header.Type == pdu.TypeOpen {
						mutex.Lock()
						opens++
						if opens == failOpen {
							responseError = pdu.ErrorOpenFailed
						}
						sessionID++
						id = sessionID
						mutex.Unlock()
					}

					data, err := (&pdu.HeaderPacket{
						Header: &pdu.Header{
							Type:          pdu.TypeResponse,
							SessionID:     id,
							TransactionID: req.Header.TransactionID,
							PacketID:      req.Header.PacketID,
						},
						Packet: &pdu.Response{Error: responseError},
					}).MarshalBinary()
					if err != nil {
						return
					}
					if _, err := conn.Write(data); err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	c, err := Dial("tcp", listener.Addr().String(),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithTimeout(2*time.Second),
		WithReconnectInterval(10*time.Millisecond))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	for i := 0; i < 2; i++ {
		if _, err := c.Session(nil, "test", nil); err != nil {
			t.Fatalf("Session %d: %v", i, err)
		}
	}

	// Drop the connection from the master agent's side, which is what a
	// restarting snmpd does: both sessions re-open, and one of the two
	// re-opens is rejected.
	(<-conns).Close()

	deadline := time.Now().Add(frameTimeout)
	for {
		c.sessionsMu.RLock()
		live := len(c.sessions)
		c.sessionsMu.RUnlock()

		if live == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after a partly failed reconnect %d sessions are live, want 1", live)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
