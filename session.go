// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package agentx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/LJS360d/go-agentx/pdu"
	"github.com/LJS360d/go-agentx/value"
)

// incomingQueueSize bounds how many requests may be waiting on a session's
// handler goroutine before the dispatcher blocks. The master agent serialises
// the PDUs of one transaction, so a queue this deep only fills up when a
// handler is much slower than the request rate.
const incomingQueueSize = 16

// Session defines an agentx session.
type Session struct {
	client  *Client
	handler Handler
	timeout time.Duration

	idMutex   sync.RWMutex
	sessionID uint32

	openRequestPacket     *pdu.HeaderPacket
	registerRequestPacket *pdu.HeaderPacket

	incoming  chan *pdu.HeaderPacket
	closeOnce sync.Once
	done      chan struct{}

	// pending holds the variable bindings that passed testSet, keyed by
	// transaction id, for handlers implementing SetHandler. The master agent
	// always terminates a transaction with cleanupSet (RFC 2741 7.2.4.3),
	// which is what removes the entry.
	pendingMutex sync.Mutex
	pending      map[uint32][]SetRequest
}

func openSession(client *Client, nameOID value.OID, name string, handler Handler) (*Session, error) {
	s := &Session{
		client:   client,
		handler:  handler,
		timeout:  client.options.timeout,
		pending:  make(map[uint32][]SetRequest),
		incoming: make(chan *pdu.HeaderPacket, incomingQueueSize),
		done:     make(chan struct{}),
	}

	requestPacket := &pdu.Open{}
	requestPacket.Timeout.Duration = s.timeout
	requestPacket.ID.SetIdentifier(nameOID)
	requestPacket.Description.Text = name
	request := &pdu.HeaderPacket{Header: &pdu.Header{Type: pdu.TypeOpen}, Packet: requestPacket}

	response, err := s.request(request)
	if err := checkError(response, err); err != nil {
		return nil, err
	}
	s.setID(response.Header.SessionID)
	s.openRequestPacket = request

	go s.run()

	return s, nil
}

// run serialises handling of the requests the master agent sends for this
// session. Handlers are user code: running them on the client's dispatcher
// goroutine would let a single slow handler stall every other session's
// traffic, and running them concurrently would break the ordering the
// testSet/commitSet/cleanupSet sequence depends on.
func (s *Session) run() {
	for {
		select {
		case <-s.done:
			return
		case <-s.client.done:
			return
		case headerPacket := <-s.incoming:
			if response := s.handle(headerPacket); response != nil {
				s.client.send(response)
			}
		}
	}
}

func (s *Session) enqueue(headerPacket *pdu.HeaderPacket) {
	select {
	case s.incoming <- headerPacket:
	case <-s.done:
	case <-s.client.done:
	}
}

// ID returns the session id.
func (s *Session) ID() uint32 {
	s.idMutex.RLock()
	defer s.idMutex.RUnlock()
	return s.sessionID
}

func (s *Session) setID(id uint32) {
	s.idMutex.Lock()
	defer s.idMutex.Unlock()
	s.sessionID = id
}

// Register registers the client under the provided rootID with the provided priority
// on the master agent.
func (s *Session) Register(priority byte, baseOID value.OID) error {
	if s.registerRequestPacket != nil {
		return fmt.Errorf("session is already registered")
	}

	requestPacket := &pdu.Register{}
	requestPacket.Timeout.Duration = s.timeout
	requestPacket.Timeout.Priority = priority
	requestPacket.Subtree.SetIdentifier(baseOID)
	request := &pdu.HeaderPacket{Header: &pdu.Header{Type: pdu.TypeRegister}, Packet: requestPacket}

	response, err := s.request(request)
	if err := checkError(response, err); err != nil {
		return err
	}
	s.registerRequestPacket = request
	return nil
}

// Unregister removes the registration for the provided subtree.
func (s *Session) Unregister(priority byte, baseOID value.OID) error {
	if s.registerRequestPacket == nil {
		return fmt.Errorf("session is not registered")
	}

	requestPacket := &pdu.Unregister{}
	requestPacket.Timeout.Priority = priority
	requestPacket.Subtree.SetIdentifier(baseOID)
	request := &pdu.HeaderPacket{Header: &pdu.Header{}, Packet: requestPacket}

	response, err := s.request(request)
	if err := checkError(response, err); err != nil {
		return err
	}
	s.registerRequestPacket = nil
	return nil
}

// Close tears down the session with the master agent.
func (s *Session) Close() error {
	requestPacket := &pdu.Close{Reason: pdu.ReasonShutdown}

	response, err := s.request(&pdu.HeaderPacket{Header: &pdu.Header{}, Packet: requestPacket})

	// The session is gone locally whether or not the master agent liked the
	// request; leaving it registered would keep routing its traffic to a
	// handler the caller believes is finished.
	s.client.unregisterSession(s.ID())
	s.closeOnce.Do(func() { close(s.done) })

	return checkError(response, err)
}

// SendTrap sends a trap/notification to the master agent.
//
// A non-zero timestamp is sent as a leading sysUpTime.0 varbind, which is how
// RFC 2741 6.2.10 conveys it - the Notify PDU has no timestamp field of its
// own. The variables must contain snmpTrapOID.0 as required by that section.
func (s *Session) SendTrap(timestamp time.Duration, variables pdu.Variables) error {
	requestPacket := &pdu.Notify{
		Timestamp: timestamp,
		Variables: variables,
	}
	request := &pdu.HeaderPacket{Header: &pdu.Header{Type: pdu.TypeNotify}, Packet: requestPacket}

	response, err := s.request(request)
	return checkError(response, err)
}

// reopen re-establishes the session on a new connection and replays its
// registration.
//
// The stored request packets are replayed through fresh headers rather than
// being resent as-is: a header is mutated in flight with the session and
// packet ids of the attempt, and the stored packet is also what a concurrent
// Register call is holding.
func (s *Session) reopen() error {
	if s.openRequestPacket != nil {
		// RFC 2741 6.2.1: an open carries no session id - the master agent
		// assigns one in its response. Sending the id of the session that just
		// died is meaningless to a master agent that has forgotten it.
		s.setID(0)

		response, err := s.request(&pdu.HeaderPacket{
			Header: &pdu.Header{Type: pdu.TypeOpen},
			Packet: s.openRequestPacket.Packet,
		})
		if err := checkError(response, err); err != nil {
			return err
		}
		s.setID(response.Header.SessionID)
	}

	if s.registerRequestPacket != nil {
		response, err := s.request(&pdu.HeaderPacket{
			Header: &pdu.Header{Type: pdu.TypeRegister},
			Packet: s.registerRequestPacket.Packet,
		})
		if err := checkError(response, err); err != nil {
			return err
		}
	}

	return nil
}

func (s *Session) request(hp *pdu.HeaderPacket) (*pdu.HeaderPacket, error) {
	hp.Header.SessionID = s.ID()
	return s.client.request(hp)
}

// handle processes one request from the master agent and returns the response
// to send back, or nil when the PDU must not be answered.
func (s *Session) handle(request *pdu.HeaderPacket) *pdu.HeaderPacket {
	responseHeader := &pdu.Header{
		SessionID:     request.Header.SessionID,
		TransactionID: request.Header.TransactionID,
		PacketID:      request.Header.PacketID,
	}
	responsePacket := &pdu.Response{}

	ctx := context.Background()
	ctx = withSessionID(ctx, request.Header.SessionID)
	ctx = withTransactionID(ctx, request.Header.TransactionID)
	ctx = withPacketID(ctx, request.Header.PacketID)

	// fail applies RFC 2741 7.2.3: on a processing failure the error and the
	// 1-based index of the offending search range are reported and the
	// VarBindList is reset to null.
	fail := func(err pdu.Error, index int) {
		responsePacket.Error = err
		responsePacket.Index = uint16(index)
		responsePacket.Variables = nil
	}

	switch requestPacket := request.Packet.(type) {
	case *pdu.Get:
		for index, searchRange := range requestPacket.SearchRanges {
			oid := searchRange.From.GetIdentifier()

			if s.handler == nil {
				s.client.logger.Warn("no handler for session specified")
				responsePacket.Variables.Add(oid, pdu.VariableTypeNoSuchObject, nil)
				continue
			}

			name, t, v, err := s.handler.Get(ctx, oid)
			if err != nil {
				s.client.logger.Error("get error", slog.String("oid", oid.String()), slog.Any("err", err))
				fail(getError(err), index+1)
				return &pdu.HeaderPacket{Header: responseHeader, Packet: responsePacket}
			}
			if name == nil {
				responsePacket.Variables.Add(oid, pdu.VariableTypeNoSuchObject, nil)
				continue
			}
			responsePacket.Variables.Add(name, t, v)
		}

	case *pdu.GetNext:
		for index, searchRange := range requestPacket.SearchRanges {
			from := searchRange.From.GetIdentifier()

			if s.handler == nil {
				s.client.logger.Warn("no handler for session specified")
				responsePacket.Variables.Add(from, pdu.VariableTypeEndOfMIBView, nil)
				continue
			}

			name, t, v, err := s.handler.GetNext(ctx, from, searchRange.From.GetInclude(), searchRange.To.GetIdentifier())
			if err != nil {
				s.client.logger.Error("get next error", slog.String("oid", from.String()), slog.Any("err", err))
				fail(getError(err), index+1)
				return &pdu.HeaderPacket{Header: responseHeader, Packet: responsePacket}
			}
			if name == nil {
				// RFC 2741 7.2.3.2 (2): v.name is the starting OID.
				responsePacket.Variables.Add(from, pdu.VariableTypeEndOfMIBView, nil)
				continue
			}
			responsePacket.Variables.Add(name, t, v)
		}

	case *pdu.TestSet:
		if s.handler == nil {
			s.client.logger.Warn("no handler for session specified")
			responsePacket.Error = pdu.ErrorNotWritable
			break
		}

		// RFC 2741 6.2.6: the response to testSet carries an error/index only,
		// its VarBindList must be empty.
		setHandler, twoPhase := s.handler.(SetHandler)
		accepted := make([]SetRequest, 0, len(requestPacket.Variables))

		for i, variable := range requestPacket.Variables {
			req := SetRequest{
				OID:   variable.Name.GetIdentifier(),
				Type:  variable.Type,
				Value: variable.Value,
			}

			var err error
			if twoPhase {
				err = setHandler.TestSet(ctx, req)
			} else {
				err = s.handler.Set(ctx, req.OID, req.Type, req.Value)
			}
			if err != nil {
				s.client.logger.Error("test set error",
					slog.String("oid", req.OID.String()),
					slog.Any("err", err))
				fail(setError(err), i+1)
				accepted = nil
				break
			}
			accepted = append(accepted, req)
		}

		if twoPhase && accepted != nil {
			s.pendingMutex.Lock()
			s.pending[request.Header.TransactionID] = accepted
			s.pendingMutex.Unlock()
		}

	case *pdu.CommitSet:
		setHandler, twoPhase := s.handler.(SetHandler)
		if !twoPhase {
			break
		}

		reqs := s.takePending(request.Header.TransactionID, false)
		if len(reqs) == 0 {
			break
		}
		if err := setHandler.CommitSet(ctx, reqs); err != nil {
			s.client.logger.Error("commit set error", slog.Any("err", err))
			// RFC 2741 7.2.4.2 allows noError or commitFailed only.
			responsePacket.Error = pdu.ErrorCommitFailed
		}

	case *pdu.UndoSet:
		setHandler, twoPhase := s.handler.(SetHandler)
		if !twoPhase {
			break
		}

		reqs := s.takePending(request.Header.TransactionID, false)
		if len(reqs) == 0 {
			break
		}
		if err := setHandler.UndoSet(ctx, reqs); err != nil {
			s.client.logger.Error("undo set error", slog.Any("err", err))
			// RFC 2741 7.2.4.3 allows noError or undoFailed only.
			responsePacket.Error = pdu.ErrorUndoFailed
		}

	case *pdu.CleanupSet:
		// RFC 2741 7.2.4.4: "No response is sent by the subagent." Terminates
		// the transaction either way.
		s.takePending(request.Header.TransactionID, true)
		return nil

	default:
		s.client.logger.Error("unable to handle packet", slog.String("packet-type", request.Header.Type.String()))
		responsePacket.Error = unsupportedError(request.Header.Type)
	}

	return &pdu.HeaderPacket{Header: responseHeader, Packet: responsePacket}
}

// takePending returns the bindings accumulated for a transaction, removing the
// entry when remove is set. commitSet and undoSet only read: undoSet is sent
// after a commit the master agent went on to abandon, so the bindings have to
// outlive commitSet. cleanupSet is what ends the transaction.
func (s *Session) takePending(transactionID uint32, remove bool) []SetRequest {
	s.pendingMutex.Lock()
	defer s.pendingMutex.Unlock()

	reqs := s.pending[transactionID]
	if remove {
		delete(s.pending, transactionID)
	}
	return reqs
}

// getError maps a Handler error onto the error-status of a get/getNext
// response. RFC 2741 7.2.3 names genErr for a processing failure; a handler
// that wants a different code can return (or wrap) a pdu.Error.
func getError(err error) pdu.Error {
	return setErrorOr(err, pdu.ErrorGenErr)
}

// setError extracts an explicit SNMP error-status from a Handler.Set error,
// falling back to genErr. Wrap with fmt.Errorf("...: %w", pdu.ErrorNotWritable)
// to keep a descriptive message and still control the code the client sees.
func setError(err error) pdu.Error {
	return setErrorOr(err, pdu.ErrorGenErr)
}

func setErrorOr(err error, fallback pdu.Error) pdu.Error {
	var e pdu.Error
	if errors.As(err, &e) && e != pdu.ErrorNone {
		return e
	}
	return fallback
}

// unsupportedError picks the error-status for a PDU type this library does not
// implement. RFC 2741 6.2.16 splits the legal values by PDU class: the "SNMP
// request processing" types (5-11) may use the SNMPv2 error-status values,
// everything else is limited to the administrative set.
func unsupportedError(t pdu.Type) pdu.Error {
	if t >= pdu.TypeGet && t <= pdu.TypeCleanupSet {
		return pdu.ErrorGenErr
	}
	return pdu.ErrorProcessing
}

// checkError turns a request/response pair into an error. The pdu.Error is
// returned as-is so that callers can match it with errors.Is/errors.As.
func checkError(hp *pdu.HeaderPacket, err error) error {
	if err != nil {
		return err
	}
	if hp == nil {
		return nil
	}
	response, ok := hp.Packet.(*pdu.Response)
	if !ok {
		return nil
	}
	if response.Error == pdu.ErrorNone {
		return nil
	}
	return response.Error
}
