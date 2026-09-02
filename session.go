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

// Session defines an agentx session.
type Session struct {
	client    *Client
	handler   Handler
	sessionID uint32
	timeout   time.Duration

	openRequestPacket     *pdu.HeaderPacket
	registerRequestPacket *pdu.HeaderPacket

	// pending holds the variable bindings that passed testSet, keyed by
	// transaction id, for handlers implementing SetHandler. The master agent
	// always terminates a transaction with cleanupSet (RFC 2741 7.2.4.3),
	// which is what removes the entry.
	pendingMutex sync.Mutex
	pending      map[uint32][]SetRequest
}

func openSession(client *Client, nameOID value.OID, name string, handler Handler) (*Session, error) {
	s := &Session{
		client:  client,
		handler: handler,
		timeout: client.options.timeout,
		pending: make(map[uint32][]SetRequest),
	}

	requestPacket := &pdu.Open{}
	requestPacket.Timeout.Duration = s.timeout
	requestPacket.ID.SetIdentifier(nameOID)
	requestPacket.Description.Text = name
	request := &pdu.HeaderPacket{Header: &pdu.Header{Type: pdu.TypeOpen}, Packet: requestPacket}

	response := s.request(request)
	if err := checkError(response); err != nil {
		return nil, err
	}
	s.sessionID = response.Header.SessionID
	s.openRequestPacket = request

	return s, nil
}

// ID returns the session id.
func (s *Session) ID() uint32 {
	return s.sessionID
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

	response := s.request(request)
	if err := checkError(response); err != nil {
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
	requestPacket.Timeout.Duration = s.timeout
	requestPacket.Timeout.Priority = priority
	requestPacket.Subtree.SetIdentifier(baseOID)
	request := &pdu.HeaderPacket{Header: &pdu.Header{}, Packet: requestPacket}

	response := s.request(request)
	if err := checkError(response); err != nil {
		return err
	}
	s.registerRequestPacket = nil
	return nil
}

// Close tears down the session with the master agent.
func (s *Session) Close() error {
	requestPacket := &pdu.Close{Reason: pdu.ReasonShutdown}

	response := s.request(&pdu.HeaderPacket{Header: &pdu.Header{}, Packet: requestPacket})
	if err := checkError(response); err != nil {
		return err
	}
	return nil
}

// SendTrap sends a trap/notification to the master agent.
func (s *Session) SendTrap(timestamp time.Duration, variables pdu.Variables) error {
	requestPacket := &pdu.Notify{
		Timestamp: timestamp,
		Variables: variables,
	}
	request := &pdu.HeaderPacket{Header: &pdu.Header{Type: pdu.TypeNotify}, Packet: requestPacket}

	response := s.request(request)
	return checkError(response)
}

func (s *Session) reopen() error {
	if s.openRequestPacket != nil {
		response := s.request(s.openRequestPacket)
		if err := checkError(response); err != nil {
			return err
		}
		s.sessionID = response.Header.SessionID
	}

	if s.registerRequestPacket != nil {
		response := s.request(s.registerRequestPacket)
		if err := checkError(response); err != nil {
			return err
		}
	}

	return nil
}

func (s *Session) request(hp *pdu.HeaderPacket) *pdu.HeaderPacket {
	hp.Header.SessionID = s.sessionID
	return s.client.request(hp)
}

func (s *Session) handle(request *pdu.HeaderPacket) *pdu.HeaderPacket {
	responseHeader := &pdu.Header{}
	responseHeader.SessionID = request.Header.SessionID
	responseHeader.TransactionID = request.Header.TransactionID
	responseHeader.PacketID = request.Header.PacketID
	responsePacket := &pdu.Response{}

	ctx := context.Background()
	ctx = withSessionID(ctx, request.Header.SessionID)
	ctx = withTransactionID(ctx, request.Header.TransactionID)
	ctx = withPacketID(ctx, request.Header.PacketID)

	switch requestPacket := request.Packet.(type) {
	case *pdu.Get:
		if s.handler == nil {
			s.client.logger.Warn("no handler for session specified")
			responsePacket.Variables.Add(requestPacket.GetOID(), pdu.VariableTypeNull, nil)
			break
		}

		oid, t, v, err := s.handler.Get(ctx, requestPacket.GetOID())
		if err != nil {
			s.client.logger.Error("packet error", slog.Any("err", err))
			responsePacket.Error = pdu.ErrorProcessing
		}
		if oid == nil {
			responsePacket.Variables.Add(requestPacket.GetOID(), pdu.VariableTypeNoSuchObject, nil)
		} else {
			responsePacket.Variables.Add(oid, t, v)
		}

	case *pdu.GetNext:
		if s.handler == nil {
			s.client.logger.Warn("no handler for session specified")
			break
		}

		for _, sr := range requestPacket.SearchRanges {
			oid, t, v, err := s.handler.GetNext(ctx, sr.From.GetIdentifier(), (sr.From.Include == 1), sr.To.GetIdentifier())
			if err != nil {
				s.client.logger.Error("packet error", slog.Any("err", err))
				responsePacket.Error = pdu.ErrorProcessing
			}

			if oid == nil {
				responsePacket.Variables.Add(sr.From.GetIdentifier(), pdu.VariableTypeEndOfMIBView, nil)
			} else {
				responsePacket.Variables.Add(oid, t, v)
			}
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
				responsePacket.Error = setError(err)
				responsePacket.Index = uint16(i + 1)
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
			responsePacket.Error = setErrorOr(err, pdu.ErrorCommitFailed)
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
			responsePacket.Error = setErrorOr(err, pdu.ErrorUndoFailed)
		}

	case *pdu.CleanupSet:
		// Terminates the transaction either way; cleanupSet has no response.
		s.takePending(request.Header.TransactionID, true)

	default:
		s.client.logger.Error("unable to handle packet", slog.String("packet-type", request.Header.Type.String()))
		responsePacket.Error = pdu.ErrorProcessing
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

func checkError(hp *pdu.HeaderPacket) error {
	response, ok := hp.Packet.(*pdu.Response)
	if !ok {
		return nil
	}
	if response.Error == pdu.ErrorNone {
		return nil
	}
	return errors.New(response.Error.String())
}
