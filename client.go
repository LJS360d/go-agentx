// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package agentx

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LJS360d/go-agentx/pdu"
	"github.com/LJS360d/go-agentx/value"
)

const (
	// defaultRequestTimeout bounds how long a caller waits for the master
	// agent's response when no timeout was configured. Without a bound, a
	// single dropped response blocks the caller for the lifetime of the
	// process.
	defaultRequestTimeout = 30 * time.Second

	// defaultReconnectInterval keeps the reconnect loop from spinning when no
	// interval was configured.
	defaultReconnectInterval = time.Second
)

// ErrRequestTimeout is returned when the master agent does not answer a
// request within the configured timeout.
var ErrRequestTimeout = errors.New("timeout waiting for the master agent's response")

// ErrClientClosed is returned when a request is made on a closed client.
var ErrClientClosed = errors.New("client is closed")

// Client defines an agentx client.
type Client struct {
	logger  *slog.Logger
	network string
	address string
	options dialOptions

	connMu sync.Mutex
	conn   net.Conn

	// tx carries already-encoded frames. Encoding happens on the caller's
	// goroutine so that a PDU which cannot be encoded fails the call that
	// made it, instead of being dropped by the writer and leaving that caller
	// waiting for a response that can never come.
	tx chan []byte

	nextPacketID atomic.Uint32

	pendingMu sync.Mutex
	pending   map[uint32]chan *pdu.HeaderPacket

	sessions   map[uint32]*Session
	sessionsMu sync.RWMutex

	errorChan   chan error
	errorMu     sync.RWMutex
	errorClosed bool

	done      chan struct{}
	closeOnce sync.Once
}

// Dial connects to the provided agentX endpoint.
func Dial(network, address string, opts ...DialOption) (*Client, error) {
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, fmt.Errorf("dial %s %s: %w", network, address, err)
	}
	return newClient(network, address, conn, opts...), nil
}

// newClient wires up a Client around an already-established conn. Split out
// of Dial so tests can drive the protocol state machine over an in-memory
// net.Pipe instead of a real socket.
func newClient(network, address string, conn net.Conn, opts ...DialOption) *Client {
	options := dialOptions{}
	for _, dialOption := range opts {
		dialOption(&options)
	}

	c := &Client{
		logger:    options.logger,
		network:   network,
		address:   address,
		options:   options,
		conn:      conn,
		tx:        make(chan []byte),
		pending:   make(map[uint32]chan *pdu.HeaderPacket),
		sessions:  make(map[uint32]*Session),
		errorChan: make(chan error, 10),
		done:      make(chan struct{}),
	}

	if c.logger == nil {
		c.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	c.runTransmitter()
	rx := c.runReceiver()
	c.runDispatcher(rx)
	c.runErrorHandler()

	return c
}

// Close tears down the client.
func (c *Client) Close() error {
	var err error

	c.closeOnce.Do(func() {
		close(c.done)

		c.errorMu.Lock()
		c.errorClosed = true
		close(c.errorChan)
		c.errorMu.Unlock()

		if closeErr := c.currentConn().Close(); closeErr != nil {
			err = fmt.Errorf("close connection: %w", closeErr)
		}
	})

	return err
}

// Error returns a channel of errors the client hit on its own goroutines,
// where there is no caller to return them to. The channel is closed by Close.
func (c *Client) Error() <-chan error {
	return c.errorChan
}

func (c *Client) currentConn() net.Conn {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	return c.conn
}

func (c *Client) setConn(conn net.Conn) {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	c.conn = conn
}

func (c *Client) isClosed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// reportError publishes a background error without ever blocking and without
// racing Close. The read lock is what makes the close in Close safe: it cannot
// proceed while a send is in flight, and no send can start once errorClosed is
// set.
func (c *Client) reportError(err error) {
	c.errorMu.RLock()
	defer c.errorMu.RUnlock()

	if c.errorClosed {
		return
	}
	select {
	case c.errorChan <- err:
	default:
	}
}

// runErrorHandler drains the error channel into the configured handler. It is
// only started when a handler is configured: the drain and Error() are two
// consumers of the same channel, and running both means each error reaches
// only one of them.
func (c *Client) runErrorHandler() {
	if c.options.errorHandler == nil {
		return
	}
	go func() {
		for err := range c.errorChan {
			c.options.errorHandler(err)
		}
	}()
}

func (c *Client) requestTimeout() time.Duration {
	if c.options.timeout > 0 {
		return c.options.timeout
	}
	return defaultRequestTimeout
}

func (c *Client) reconnectInterval() time.Duration {
	if c.options.reconnectInterval > 0 {
		return c.options.reconnectInterval
	}
	return defaultReconnectInterval
}

// Session sets up a new session.
func (c *Client) Session(nameOID value.OID, name string, handler Handler) (*Session, error) {
	s, err := openSession(c, nameOID, name, handler)
	if err != nil {
		return nil, fmt.Errorf("open session: %w", err)
	}
	c.registerSession(s)
	return s, nil
}

func (c *Client) registerSession(s *Session) {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	c.sessions[s.ID()] = s
}

func (c *Client) unregisterSession(id uint32) {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	delete(c.sessions, id)
}

func (c *Client) session(id uint32) (*Session, bool) {
	c.sessionsMu.RLock()
	defer c.sessionsMu.RUnlock()
	s, ok := c.sessions[id]
	return s, ok
}

// send encodes a packet and queues it for transmission. It reports false if
// the client was closed, or if the packet could not be encoded - the latter
// cannot happen for the error responses this library builds itself, but a
// response carrying handler-supplied values can fail, and a master agent left
// with no reply at all would simply time the session out.
func (c *Client) send(hp *pdu.HeaderPacket) bool {
	data, err := hp.MarshalBinary()
	if err != nil {
		c.logger.Error("packet marshal error", slog.Any("err", err))
		c.reportError(fmt.Errorf("packet marshal error: %w", err))

		fallback, fallbackErr := errorResponse(hp.Header, pdu.ErrorGenErr).MarshalBinary()
		if fallbackErr != nil {
			return false
		}
		data = fallback
	}
	return c.sendBytes(data)
}

// sendBytes queues an already-encoded frame.
func (c *Client) sendBytes(data []byte) bool {
	select {
	case c.tx <- data:
		return true
	case <-c.done:
		return false
	}
}

func (c *Client) runTransmitter() {
	go func() {
		for {
			select {
			case <-c.done:
				return
			case data := <-c.tx:
				if _, err := c.currentConn().Write(data); err != nil {
					c.logger.Debug("header packet write error", slog.Any("err", err))
					c.reportError(fmt.Errorf("header packet write error: %w", err))
					continue
				}
			}
		}
	}()
}

func (c *Client) runReceiver() chan *pdu.HeaderPacket {
	rx := make(chan *pdu.HeaderPacket)

	go func() {
		reader := bufio.NewReader(c.currentConn())

		for {
			headerPacket, err := readHeaderPacket(reader)
			if err != nil {
				if c.isClosed() || errors.Is(err, net.ErrClosed) {
					return
				}

				var parseErr *parseError
				if errors.As(err, &parseErr) {
					// RFC 2741 7.2.2: a PDU whose header parsed but whose
					// payload did not is answered with parseError. Dropping the
					// connection instead - as this used to - leaves the subagent
					// silently deaf for the rest of the process's life.
					c.logger.Error("packet parse error", slog.Any("err", parseErr))
					c.reportError(fmt.Errorf("packet parse error: %w", parseErr))
					if response := parseErrorResponse(parseErr.header); response != nil {
						c.send(response)
					}
					continue
				}

				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
					newReader, ok := c.reconnect()
					if !ok {
						return
					}
					reader = newReader
					continue
				}

				c.reportError(fmt.Errorf("header read error: %w", err))
				return
			}

			select {
			case rx <- headerPacket:
			case <-c.done:
				return
			}
		}
	}()

	return rx
}

// reconnect re-dials the master agent and re-opens every session that was
// established before the connection dropped.
func (c *Client) reconnect() (*bufio.Reader, bool) {
	c.logger.Info("lost connection", slog.Duration("re-connect-in", c.reconnectInterval()))

	for {
		select {
		case <-time.After(c.reconnectInterval()):
		case <-c.done:
			return nil, false
		}

		conn, err := net.Dial(c.network, c.address)
		if err != nil {
			c.logger.Error("re-connect error", slog.Any("err", err))
			c.reportError(fmt.Errorf("re-connect error: %w", err))
			continue
		}

		c.setConn(conn)

		// The re-open handshake is itself a request/response exchange, so it
		// has to run off the receiver goroutine that will carry its responses.
		go c.reopenSessions()

		return bufio.NewReader(conn), true
	}
}

// reopenSessions replays the open and registration of every session on the
// new connection. RFC 2741 7.1.8: a master agent keeps no state for a closed
// session, so "if the subagent wishes to re-establish the session after it has
// been closed, it needs to re-register the MIB regions".
//
// The sessions are removed from the routing table first, because their old
// session ids are meaningless to the new connection - a request arriving under
// one belongs to nothing, and must be answered with notOpen rather than
// dispatched to a handler.
func (c *Client) reopenSessions() {
	c.sessionsMu.Lock()
	sessions := make([]*Session, 0, len(c.sessions))
	for _, session := range c.sessions {
		sessions = append(sessions, session)
	}
	c.sessions = make(map[uint32]*Session)
	c.sessionsMu.Unlock()

	reopened := 0
	for _, session := range sessions {
		// One session failing to re-open must not abandon the others: they are
		// independent, and giving up here used to leave every session after
		// the failing one permanently unreachable.
		if err := session.reopen(); err != nil {
			c.logger.Error("re-open error", slog.Any("err", err))
			c.reportError(fmt.Errorf("session re-open error: %w", err))
			continue
		}
		c.registerSession(session)
		reopened++
	}

	c.logger.Info("re-connect successful",
		slog.Int("sessions-reopened", reopened),
		slog.Int("sessions-lost", len(sessions)-reopened))
}

// parseError carries the header of a PDU whose payload could not be decoded,
// so that a parseError response can still be addressed correctly.
type parseError struct {
	header *pdu.Header
	err    error
}

func (p *parseError) Error() string { return p.err.Error() }
func (p *parseError) Unwrap() error { return p.err }

// readHeaderPacket reads one full PDU off the wire. A failure to decode the
// payload is reported as *parseError, which is recoverable; anything else
// means the stream itself can no longer be trusted or has ended.
func readHeaderPacket(reader *bufio.Reader) (*pdu.HeaderPacket, error) {
	headerBytes := make([]byte, pdu.HeaderSize)
	if _, err := io.ReadFull(reader, headerBytes); err != nil {
		return nil, err
	}

	header := &pdu.Header{}
	if err := header.UnmarshalBinary(headerBytes); err != nil {
		// UnmarshalBinary fills the header in before validating it, so the
		// payload length is available here even though the header was
		// rejected. When that length is itself plausible the payload can be
		// skipped and the stream stays framed; when it is not, there is no way
		// to find the next PDU boundary and the connection is finished.
		if header.PayloadLength%4 != 0 || header.PayloadLength > pdu.MaxPayloadLength {
			return nil, fmt.Errorf("header unmarshal error: %w", err)
		}
		if _, discardErr := io.CopyN(io.Discard, reader, int64(header.PayloadLength)); discardErr != nil {
			return nil, discardErr
		}
		return nil, &parseError{header: header, err: err}
	}

	packetBytes := make([]byte, header.PayloadLength)
	if _, err := io.ReadFull(reader, packetBytes); err != nil {
		return nil, err
	}

	packet := packetForType(header.Type)
	if err := unmarshalPacket(packet, packetBytes, header.ByteOrder()); err != nil {
		return nil, &parseError{header: header, err: err}
	}

	return &pdu.HeaderPacket{Header: header, Packet: packet}, nil
}

func packetForType(t pdu.Type) pdu.Packet {
	switch t {
	case pdu.TypeResponse:
		return &pdu.Response{}
	case pdu.TypeGet:
		return &pdu.Get{}
	case pdu.TypeGetNext:
		return &pdu.GetNext{}
	case pdu.TypeTestSet:
		return &pdu.TestSet{}
	case pdu.TypeCommitSet:
		return &pdu.CommitSet{}
	case pdu.TypeUndoSet:
		return &pdu.UndoSet{}
	case pdu.TypeCleanupSet:
		return &pdu.CleanupSet{}
	case pdu.TypeNotify:
		return &pdu.Notify{}
	}

	// RFC 2741 7.2.4.1: a type this library does not implement (GetBulk,
	// Ping, ...) is answered with an error, not dropped or crashed on -
	// pdu.Unsupported keeps the wire framing in sync.
	return &pdu.Unsupported{PacketType: t}
}

// orderedUnmarshaler is implemented by the payload types whose encoding
// contains multi-byte fields. RFC 2741 5 makes the header's
// NETWORK_BYTE_ORDER bit govern all of them, not just the header's own.
type orderedUnmarshaler interface {
	UnmarshalBinaryOrder(data []byte, order binary.ByteOrder) error
}

func unmarshalPacket(packet pdu.Packet, data []byte, order binary.ByteOrder) error {
	if ordered, ok := packet.(orderedUnmarshaler); ok {
		return ordered.UnmarshalBinaryOrder(data, order)
	}
	return packet.UnmarshalBinary(data)
}

// parseErrorResponse builds the RFC 2741 7.2.2 parseError reply. A PDU that
// expects no response at all gets none.
func parseErrorResponse(header *pdu.Header) *pdu.HeaderPacket {
	if !expectsResponse(header.Type) {
		return nil
	}
	return errorResponse(header, pdu.ErrorParse)
}

func errorResponse(header *pdu.Header, err pdu.Error) *pdu.HeaderPacket {
	return &pdu.HeaderPacket{
		Header: &pdu.Header{
			SessionID:     header.SessionID,
			TransactionID: header.TransactionID,
			PacketID:      header.PacketID,
		},
		Packet: &pdu.Response{Error: err},
	}
}

// expectsResponse reports whether the subagent must answer a PDU of this type.
// RFC 2741 6.2.9 / 7.2.4.4: the agentx-CleanupSet-PDU is the one request that
// is never answered. A Response is not a request in the first place.
func expectsResponse(t pdu.Type) bool {
	return t != pdu.TypeCleanupSet && t != pdu.TypeResponse
}

func (c *Client) runDispatcher(rx chan *pdu.HeaderPacket) {
	go func() {
		for {
			select {
			case <-c.done:
				return

			case headerPacket := <-rx:
				if headerPacket.Header.Type == pdu.TypeResponse {
					// RFC 2741 7.2.2: a response that cannot be matched to a
					// request of ours is silently dropped. It is never handler
					// input.
					if responseChan, ok := c.takePending(headerPacket.Header.PacketID); ok {
						// The channel is buffered, so a caller that has
						// already given up cannot block the dispatcher here.
						responseChan <- headerPacket
					} else {
						c.logger.Debug("unmatched response",
							slog.Uint64("packet-id", uint64(headerPacket.Header.PacketID)))
					}
					continue
				}

				session, ok := c.session(headerPacket.Header.SessionID)
				if !ok {
					// RFC 2741 7.2.2: "if h.sessionID does not correspond to a
					// currently established session, res.error is set to
					// `notOpen'".
					c.logger.Error("got packet without session",
						slog.String("packet-type", headerPacket.Header.Type.String()),
						slog.Uint64("session-id", uint64(headerPacket.Header.SessionID)))
					if expectsResponse(headerPacket.Header.Type) {
						c.send(errorResponse(headerPacket.Header, pdu.ErrorNotOpen))
					}
					continue
				}

				// Handling runs on the session's own goroutine: it calls into
				// user code, and doing that inline would let one slow handler
				// stall response routing for every session on this client.
				session.enqueue(headerPacket)
			}
		}
	}()
}

func (c *Client) addPending(packetID uint32, responseChan chan *pdu.HeaderPacket) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pending[packetID] = responseChan
}

// takePending removes and returns the channel waiting on a packet id, if the
// request is still outstanding.
func (c *Client) takePending(packetID uint32) (chan *pdu.HeaderPacket, bool) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	responseChan, ok := c.pending[packetID]
	delete(c.pending, packetID)
	return responseChan, ok
}

// request sends a packet and waits for the matching response.
func (c *Client) request(hp *pdu.HeaderPacket) (*pdu.HeaderPacket, error) {
	// RFC 2741 6.1: every PDU except a response carries a packet id generated
	// by its sender, and the response echoes it back. It is what correlates
	// the two.
	packetID := c.nextPacketID.Add(1) - 1
	hp.Header.PacketID = packetID

	// Encoding here, rather than on the writer goroutine, is what makes an
	// unencodable PDU an immediate error for this caller instead of a silent
	// drop followed by a wait for a response nobody will send.
	data, err := hp.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", hp.Header.Type, err)
	}

	// Buffered: the dispatcher must be able to deliver a late response even
	// after this call has timed out and stopped listening.
	responseChan := make(chan *pdu.HeaderPacket, 1)
	c.addPending(packetID, responseChan)
	defer c.takePending(packetID)

	if !c.sendBytes(data) {
		return nil, ErrClientClosed
	}

	timer := time.NewTimer(c.requestTimeout())
	defer timer.Stop()

	select {
	case headerPacket := <-responseChan:
		return headerPacket, nil
	case <-timer.C:
		return nil, ErrRequestTimeout
	case <-c.done:
		return nil, ErrClientClosed
	}
}
