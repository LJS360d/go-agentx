// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package agentx

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/LJS360d/go-agentx/pdu"
	"github.com/LJS360d/go-agentx/value"
)

// testHandler is a Handler whose behaviour each test supplies. A nil field
// means "not exercised by this test" and fails loudly if it is called anyway.
type testHandler struct {
	get     func(oid value.OID) (value.OID, pdu.VariableType, any, error)
	getNext func(from value.OID, includeFrom bool, to value.OID) (value.OID, pdu.VariableType, any, error)
	set     func(oid value.OID, t pdu.VariableType, v any) error
}

func (h *testHandler) Get(_ context.Context, oid value.OID) (value.OID, pdu.VariableType, any, error) {
	if h.get == nil {
		return nil, pdu.VariableTypeNoSuchObject, nil, nil
	}
	return h.get(oid)
}

func (h *testHandler) GetNext(_ context.Context, from value.OID, includeFrom bool, to value.OID) (value.OID, pdu.VariableType, any, error) {
	if h.getNext == nil {
		return nil, pdu.VariableTypeNoSuchObject, nil, nil
	}
	return h.getNext(from, includeFrom, to)
}

func (h *testHandler) Set(_ context.Context, oid value.OID, t pdu.VariableType, v any) error {
	if h.set == nil {
		return nil
	}
	return h.set(oid, t, v)
}

// RFC 2741 6.2.5 and 7.2.3: a Get carries a SearchRangeList and "a conformant
// AgentX subagent must support multiple variables supplied in each PDU", with
// one VarBind added "in the corresponding location" of the response. The
// previous Get type held a single SearchRange, so every OID after the first
// was silently dropped.
func TestGetAnswersEverySearchRange(t *testing.T) {
	c, m := newPipeClient(t)

	handler := &testHandler{
		get: func(oid value.OID) (value.OID, pdu.VariableType, any, error) {
			// 1.3.6.1.4.1.42.N answers with the integer N; anything else is
			// not instantiated.
			if len(oid) != 8 {
				return nil, pdu.VariableTypeNoSuchObject, nil, nil
			}
			return oid, pdu.VariableTypeInteger, int32(oid[7]), nil
		},
	}
	session := m.openSession(t, c, 1, handler)

	base := value.OID{1, 3, 6, 1, 4, 1, 42}
	m.send(masterRequest(pdu.TypeGet, session.ID(), 1, 1, getOf(
		append(append(value.OID{}, base...), 1),
		append(append(value.OID{}, base...), 2),
		value.OID{1, 3, 6, 1, 4, 1, 43}, // not instantiated
	)))

	response := responseFrom(t, m.recv(t))
	if response.Error != pdu.ErrorNone {
		t.Fatalf("response error = %v, want ErrorNone", response.Error)
	}
	if len(response.Variables) != 3 {
		t.Fatalf("got %d varbinds, want 3 (one per search range)", len(response.Variables))
	}
	if response.Variables[0].Value != int32(1) || response.Variables[1].Value != int32(2) {
		t.Fatalf("varbind values = %v, %v; want 1, 2",
			response.Variables[0].Value, response.Variables[1].Value)
	}
	// RFC 2741 7.2.3.1 (3): an OID that names nothing gets noSuchObject, and
	// v.name is still the requested OID.
	if response.Variables[2].Type != pdu.VariableTypeNoSuchObject {
		t.Fatalf("third varbind type = %v, want VariableTypeNoSuchObject", response.Variables[2].Type)
	}
	if got := response.Variables[2].Name.GetIdentifier().String(); got != "1.3.6.1.4.1.43" {
		t.Fatalf("third varbind name = %s, want the requested OID", got)
	}
}

// RFC 2741 7.2.3.2 (2): when no variable can be located, v.name is the
// starting OID of the search range and the VarBind is endOfMibView.
func TestGetNextReportsEndOfMibView(t *testing.T) {
	c, m := newPipeClient(t)
	session := m.openSession(t, c, 1, &testHandler{})

	from := value.OID{1, 3, 6, 1, 4, 1, 42}
	m.send(masterRequest(pdu.TypeGetNext, session.ID(), 1, 1, getNextOf([2]value.OID{from, {1, 3, 6, 1, 4, 1, 43}})))

	response := responseFrom(t, m.recv(t))
	if len(response.Variables) != 1 {
		t.Fatalf("got %d varbinds, want 1", len(response.Variables))
	}
	if response.Variables[0].Type != pdu.VariableTypeEndOfMIBView {
		t.Fatalf("varbind type = %v, want VariableTypeEndOfMIBView", response.Variables[0].Type)
	}
	if got := response.Variables[0].Name.GetIdentifier().String(); got != from.String() {
		t.Fatalf("varbind name = %s, want the starting OID %s", got, from)
	}
}

// The include flag and the ending OID of each search range have to reach the
// handler: they are what distinguish a getNext from a get, and what bounds the
// walk (RFC 2741 5.2, 7.2.3.2).
func TestGetNextPassesTheSearchRangeThrough(t *testing.T) {
	c, m := newPipeClient(t)

	type call struct {
		from        value.OID
		includeFrom bool
		to          value.OID
	}
	calls := make(chan call, 4)

	handler := &testHandler{
		getNext: func(from value.OID, includeFrom bool, to value.OID) (value.OID, pdu.VariableType, any, error) {
			calls <- call{from, includeFrom, to}
			return from, pdu.VariableTypeInteger, int32(1), nil
		},
	}
	session := m.openSession(t, c, 1, handler)

	get := getNextOf([2]value.OID{{1, 3, 6, 1}, {1, 3, 6, 2}})
	get.SearchRanges[0].From.SetInclude(true)
	// A second range with a null ending OID: RFC 2741 5.2 makes that unbounded.
	unbounded := pdu.Range{}
	unbounded.From.SetIdentifier(value.OID{1, 3, 6, 9})
	get.SearchRanges = append(get.SearchRanges, unbounded)

	m.send(masterRequest(pdu.TypeGetNext, session.ID(), 1, 1, get))
	response := responseFrom(t, m.recv(t))
	if len(response.Variables) != 2 {
		t.Fatalf("got %d varbinds, want 2", len(response.Variables))
	}

	first := <-calls
	if !first.includeFrom {
		t.Fatal("include flag was not passed to the handler")
	}
	if first.to.String() != "1.3.6.2" {
		t.Fatalf("ending OID = %s, want 1.3.6.2", first.to)
	}

	second := <-calls
	if second.includeFrom {
		t.Fatal("include flag was set for a range that did not have it")
	}
	if len(second.to) != 0 {
		t.Fatalf("ending OID = %v, want the empty OID for an unbounded range", second.to)
	}
}

// RFC 2741 7.2.3: "If processing should fail for any reason not described
// below, res.error is set to `genErr', res.index is set to the index of the
// failed SearchRange, the VarBindList is reset to null". Shipping the varbinds
// collected so far alongside an error is not a legal response.
func TestGetErrorClearsTheVarBindList(t *testing.T) {
	c, m := newPipeClient(t)

	handler := &testHandler{
		get: func(oid value.OID) (value.OID, pdu.VariableType, any, error) {
			if oid.String() == "1.3.6.1.4.1.2" {
				return nil, 0, nil, errors.New("boom")
			}
			return oid, pdu.VariableTypeInteger, int32(1), nil
		},
	}
	session := m.openSession(t, c, 1, handler)

	m.send(masterRequest(pdu.TypeGet, session.ID(), 1, 1, getOf(
		value.OID{1, 3, 6, 1, 4, 1, 1},
		value.OID{1, 3, 6, 1, 4, 1, 2},
		value.OID{1, 3, 6, 1, 4, 1, 3},
	)))

	response := responseFrom(t, m.recv(t))
	if response.Error != pdu.ErrorGenErr {
		t.Fatalf("response error = %v, want ErrorGenErr", response.Error)
	}
	if response.Index != 2 {
		t.Fatalf("res.index = %d, want 2 (1-based index of the failed search range)", response.Index)
	}
	if len(response.Variables) != 0 {
		t.Fatalf("response carries %d varbinds, want none", len(response.Variables))
	}
}

// A handler can pick the exact error-status by returning (or wrapping) a
// pdu.Error.
func TestHandlerCanChooseTheErrorStatus(t *testing.T) {
	c, m := newPipeClient(t)

	handler := &testHandler{
		get: func(oid value.OID) (value.OID, pdu.VariableType, any, error) {
			return nil, 0, nil, fmt.Errorf("no access to %s: %w", oid, pdu.ErrorNoAccess)
		},
	}
	session := m.openSession(t, c, 1, handler)

	m.send(masterRequest(pdu.TypeGet, session.ID(), 1, 1, getOf(value.OID{1, 3, 6, 1})))

	if response := responseFrom(t, m.recv(t)); response.Error != pdu.ErrorNoAccess {
		t.Fatalf("response error = %v, want ErrorNoAccess", response.Error)
	}
}

// setRecorder implements the two-phase SetHandler and records the order of the
// calls it receives.
type setRecorder struct {
	testHandler

	mutex sync.Mutex
	calls []string

	testSetErr   error
	commitSetErr error
}

func (s *setRecorder) record(name string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.calls = append(s.calls, name)
}

func (s *setRecorder) recorded() []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return append([]string(nil), s.calls...)
}

func (s *setRecorder) TestSet(_ context.Context, req SetRequest) error {
	s.record("test:" + req.OID.String())
	return s.testSetErr
}

func (s *setRecorder) CommitSet(_ context.Context, reqs []SetRequest) error {
	s.record(fmt.Sprintf("commit:%d", len(reqs)))
	return s.commitSetErr
}

func (s *setRecorder) UndoSet(_ context.Context, reqs []SetRequest) error {
	s.record(fmt.Sprintf("undo:%d", len(reqs)))
	return nil
}

// RFC 2741 7.2.4: the four set PDUs "are used collectively to perform the
// indicated management operation", and 7.2.4.4 is explicit that the
// agentx-CleanupSet-PDU gets no reply at all. Answering it puts an
// unsolicited Response on the wire, which the master agent cannot match to any
// request of its own.
func TestSetTransactionOrderingAndCleanupSilence(t *testing.T) {
	c, m := newPipeClient(t)

	handler := &setRecorder{}
	session := m.openSession(t, c, 1, handler)

	const transactionID = 77

	testSet := &pdu.TestSet{}
	testSet.Variables.Add(value.OID{1, 3, 6, 1, 4, 1, 1}, pdu.VariableTypeInteger, int32(1))
	testSet.Variables.Add(value.OID{1, 3, 6, 1, 4, 1, 2}, pdu.VariableTypeInteger, int32(2))

	m.send(masterRequest(pdu.TypeTestSet, session.ID(), transactionID, 1, testSet))
	if response := responseFrom(t, m.recv(t)); response.Error != pdu.ErrorNone {
		t.Fatalf("testSet response error = %v, want ErrorNone", response.Error)
	}

	m.send(masterRequest(pdu.TypeCommitSet, session.ID(), transactionID, 2, &pdu.CommitSet{}))
	if response := responseFrom(t, m.recv(t)); response.Error != pdu.ErrorNone {
		t.Fatalf("commitSet response error = %v, want ErrorNone", response.Error)
	}

	m.send(masterRequest(pdu.TypeCleanupSet, session.ID(), transactionID, 3, &pdu.CleanupSet{}))
	m.expectSilence(t, 200*time.Millisecond)

	// Liveness after the unanswered cleanupSet: an ordinary request still gets
	// through, which also proves the silence above was not simply a stall.
	m.send(masterRequest(pdu.TypeGet, session.ID(), 78, 4, getOf(value.OID{1, 3, 6, 1})))
	if got := m.recv(t).Header.TransactionID; got != 78 {
		t.Fatalf("transaction id = %d, want 78", got)
	}

	want := []string{"test:1.3.6.1.4.1.1", "test:1.3.6.1.4.1.2", "commit:2"}
	got := handler.recorded()
	if len(got) != len(want) {
		t.Fatalf("handler calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("handler calls = %v, want %v", got, want)
		}
	}
}

// RFC 2741 7.2.4.3: an undoSet follows a commitSet the master agent has
// decided to abandon, so the bindings from the testSet phase must still be
// available - only the cleanupSet ends the transaction.
func TestUndoSetSeesTheTestSetBindings(t *testing.T) {
	c, m := newPipeClient(t)

	handler := &setRecorder{}
	session := m.openSession(t, c, 1, handler)

	const transactionID = 88

	testSet := &pdu.TestSet{}
	testSet.Variables.Add(value.OID{1, 3, 6, 1, 4, 1, 1}, pdu.VariableTypeInteger, int32(1))

	m.send(masterRequest(pdu.TypeTestSet, session.ID(), transactionID, 1, testSet))
	m.recv(t)
	m.send(masterRequest(pdu.TypeCommitSet, session.ID(), transactionID, 2, &pdu.CommitSet{}))
	m.recv(t)
	m.send(masterRequest(pdu.TypeUndoSet, session.ID(), transactionID, 3, &pdu.UndoSet{}))
	m.recv(t)

	want := []string{"test:1.3.6.1.4.1.1", "commit:1", "undo:1"}
	got := handler.recorded()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("handler calls = %v, want %v", got, want)
	}
}

// RFC 2741 7.2.4.1: "res.index field must be set to the index of the VarBind
// for which validation failed", and 5.4 makes those indices 1-based. The
// response to a testSet carries no VarBindList.
func TestTestSetReportsTheFailingIndex(t *testing.T) {
	c, m := newPipeClient(t)

	handler := &setRecorder{testSetErr: pdu.ErrorWrongValue}
	session := m.openSession(t, c, 1, handler)

	testSet := &pdu.TestSet{}
	testSet.Variables.Add(value.OID{1, 3, 6, 1, 4, 1, 1}, pdu.VariableTypeInteger, int32(1))

	m.send(masterRequest(pdu.TypeTestSet, session.ID(), 5, 1, testSet))

	response := responseFrom(t, m.recv(t))
	if response.Error != pdu.ErrorWrongValue {
		t.Fatalf("response error = %v, want ErrorWrongValue", response.Error)
	}
	if response.Index != 1 {
		t.Fatalf("res.index = %d, want 1", response.Index)
	}
	if len(response.Variables) != 0 {
		t.Fatalf("testSet response carries %d varbinds; RFC 2741 6.2.16 wants none", len(response.Variables))
	}
}

// RFC 2741 7.2.4.2 allows exactly noError or commitFailed in a commitSet
// response - a handler error must be mapped onto commitFailed rather than
// passed through.
func TestCommitSetFailureIsCommitFailed(t *testing.T) {
	c, m := newPipeClient(t)

	handler := &setRecorder{commitSetErr: errors.New("disk full")}
	session := m.openSession(t, c, 1, handler)

	testSet := &pdu.TestSet{}
	testSet.Variables.Add(value.OID{1, 3, 6, 1, 4, 1, 1}, pdu.VariableTypeInteger, int32(1))

	m.send(masterRequest(pdu.TypeTestSet, session.ID(), 9, 1, testSet))
	m.recv(t)
	m.send(masterRequest(pdu.TypeCommitSet, session.ID(), 9, 2, &pdu.CommitSet{}))

	if response := responseFrom(t, m.recv(t)); response.Error != pdu.ErrorCommitFailed {
		t.Fatalf("response error = %v, want ErrorCommitFailed", response.Error)
	}
}

// A handler that implements only Handler keeps the single-phase behaviour:
// Set runs during the testSet phase.
func TestSinglePhaseHandlerAppliesDuringTestSet(t *testing.T) {
	c, m := newPipeClient(t)

	applied := make(chan value.OID, 1)
	handler := &testHandler{
		set: func(oid value.OID, _ pdu.VariableType, _ any) error {
			applied <- oid
			return nil
		},
	}
	session := m.openSession(t, c, 1, handler)

	testSet := &pdu.TestSet{}
	testSet.Variables.Add(value.OID{1, 3, 6, 1, 4, 1, 1}, pdu.VariableTypeInteger, int32(1))

	m.send(masterRequest(pdu.TypeTestSet, session.ID(), 1, 1, testSet))
	if response := responseFrom(t, m.recv(t)); response.Error != pdu.ErrorNone {
		t.Fatalf("response error = %v, want ErrorNone", response.Error)
	}

	select {
	case oid := <-applied:
		if oid.String() != "1.3.6.1.4.1.1" {
			t.Fatalf("Set called for %s", oid)
		}
	case <-time.After(frameTimeout):
		t.Fatal("Set was never called")
	}
}

// RFC 2741 6.2.10: the notification's timestamp travels as a leading
// sysUpTime.0 varbind, not as a field of its own. This asserts what actually
// goes on the wire.
func TestSendTrapEncodesSysUpTimeAsAVarBind(t *testing.T) {
	c, m := newPipeClient(t)
	session := m.openSession(t, c, 1, nil)

	var variables pdu.Variables
	variables.Add(pdu.OIDSnmpTrapOID, pdu.VariableTypeObjectIdentifier, "1.3.6.1.4.1.42.0.1")
	variables.Add(value.OID{1, 3, 6, 1, 4, 1, 42, 1, 0}, pdu.VariableTypeInteger, int32(7))

	done := make(chan error, 1)
	go func() { done <- session.SendTrap(42*time.Second, variables) }()

	req := m.recv(t)
	if req.Header.Type != pdu.TypeNotify {
		t.Fatalf("frame type = %v, want TypeNotify", req.Header.Type)
	}
	notify, ok := req.Packet.(*pdu.Notify)
	if !ok {
		t.Fatalf("packet has type %T, want *pdu.Notify", req.Packet)
	}
	if len(notify.Variables) != 3 {
		t.Fatalf("notify carries %d varbinds, want 3", len(notify.Variables))
	}
	if got := notify.Variables[0].Name.GetIdentifier().String(); got != pdu.OIDSysUpTime.String() {
		t.Fatalf("first varbind = %s, want sysUpTime.0", got)
	}
	if notify.Variables[0].Value != 42*time.Second {
		t.Fatalf("sysUpTime.0 = %v, want 42s", notify.Variables[0].Value)
	}
	if got := notify.Variables[1].Name.GetIdentifier().String(); got != pdu.OIDSnmpTrapOID.String() {
		t.Fatalf("second varbind = %s, want snmpTrapOID.0", got)
	}

	m.respond(req, session.ID(), pdu.ErrorNone)
	if err := <-done; err != nil {
		t.Fatalf("SendTrap: %v", err)
	}
}

// A trap without snmpTrapOID.0 is rejected by a master agent (RFC 2741
// 7.1.10), which silently generates no notification. Reporting it as an error
// is more useful than sending it into a void.
func TestSendTrapRejectsAMissingTrapOID(t *testing.T) {
	c, m := newPipeClient(t)
	session := m.openSession(t, c, 1, nil)

	var variables pdu.Variables
	variables.Add(value.OID{1, 3, 6, 1, 4, 1, 42, 1, 0}, pdu.VariableTypeInteger, int32(7))

	// The error has to be immediate. Encoding used to happen on the writer
	// goroutine, which simply dropped an unencodable PDU - leaving this call
	// waiting for a response that could never arrive, until it timed out.
	done := make(chan error, 1)
	go func() { done <- session.SendTrap(0, variables) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("SendTrap accepted a notification without snmpTrapOID.0")
		}
		if errors.Is(err, ErrRequestTimeout) {
			t.Fatalf("SendTrap failed by timing out rather than by rejecting the notification: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SendTrap did not return promptly for an unencodable notification")
	}

	m.expectSilence(t, 100*time.Millisecond)
}

// The error a master agent reports has to reach the caller as a pdu.Error, so
// that it can be matched rather than string-compared.
func TestResponseErrorIsTyped(t *testing.T) {
	c, m := newPipeClient(t)

	go func() {
		req, ok := <-m.frames
		if !ok {
			return
		}
		m.respond(req, 0, pdu.ErrorOpenFailed)
	}()

	_, err := c.Session(nil, "test", nil)
	if err == nil {
		t.Fatal("Session succeeded, want an error")
	}
	var e pdu.Error
	if !errors.As(err, &e) || e != pdu.ErrorOpenFailed {
		t.Fatalf("Session error = %v (%T), want a pdu.Error of ErrorOpenFailed", err, err)
	}
}

// A closed session must stop receiving traffic: leaving it in the client's
// map keeps dispatching requests to a handler the caller believes is done.
func TestCloseUnregistersTheSession(t *testing.T) {
	c, m := newPipeClient(t)
	session := m.openSession(t, c, 1, nil)

	served := m.serve(t, 1, session.ID())
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	<-served

	if _, ok := c.session(session.ID()); ok {
		t.Fatal("session is still registered with the client after Close")
	}

	// Traffic for it now gets the notOpen of RFC 2741 7.2.2.
	m.send(masterRequest(pdu.TypeGet, session.ID(), 1, 1, getOf(value.OID{1, 3, 6, 1})))
	if response := responseFrom(t, m.recv(t)); response.Error != pdu.ErrorNotOpen {
		t.Fatalf("response error = %v, want ErrorNotOpen", response.Error)
	}
}

// A slow handler on one session must not stop another session's traffic being
// answered. Handling used to run inline on the single dispatcher goroutine.
func TestSlowHandlerDoesNotStallOtherSessions(t *testing.T) {
	c, m := newPipeClient(t)

	release := make(chan struct{})
	slow := &testHandler{
		get: func(oid value.OID) (value.OID, pdu.VariableType, any, error) {
			<-release
			return oid, pdu.VariableTypeInteger, int32(1), nil
		},
	}
	fast := &testHandler{
		get: func(oid value.OID) (value.OID, pdu.VariableType, any, error) {
			return oid, pdu.VariableTypeInteger, int32(2), nil
		},
	}

	slowSession := m.openSession(t, c, 1, slow)
	fastSession := m.openSession(t, c, 2, fast)

	m.send(masterRequest(pdu.TypeGet, slowSession.ID(), 1, 1, getOf(value.OID{1, 3, 6, 1})))
	m.send(masterRequest(pdu.TypeGet, fastSession.ID(), 2, 2, getOf(value.OID{1, 3, 6, 1})))

	frame := m.recv(t)
	if frame.Header.TransactionID != 2 {
		t.Fatalf("first answer was for transaction %d; the slow handler blocked the dispatcher", frame.Header.TransactionID)
	}

	close(release)
	if got := m.recv(t).Header.TransactionID; got != 1 {
		t.Fatalf("second answer was for transaction %d, want 1", got)
	}
}

// RFC 2741 6.2.3/6.2.4: a registration is identified by its subtree and
// priority, so an unregister has to carry the same priority back - and the
// library must not let a caller unregister a session that never registered.
func TestRegisterAndUnregister(t *testing.T) {
	c, m := newPipeClient(t)
	session := m.openSession(t, c, 1, nil)

	if err := session.Unregister(127, value.MustParseOID("1.3.6.1.4.1.45995")); err == nil {
		t.Fatal("Unregister succeeded on a session that was never registered")
	}

	served := m.serve(t, 1, session.ID())
	if err := session.Register(127, value.MustParseOID("1.3.6.1.4.1.45995")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	registerReq := <-served
	if registerReq.Header.Type != pdu.TypeRegister {
		t.Fatalf("frame type = %v, want TypeRegister", registerReq.Header.Type)
	}
	register, ok := registerReq.Packet.(*pdu.Register)
	if !ok {
		t.Fatalf("packet has type %T, want *pdu.Register", registerReq.Packet)
	}
	if register.Timeout.Priority != 127 {
		t.Fatalf("register priority = %d, want 127", register.Timeout.Priority)
	}
	if got := register.Subtree.GetIdentifier().String(); got != "1.3.6.1.4.1.45995" {
		t.Fatalf("register subtree = %s", got)
	}

	// Registering twice is refused: the second registration would have to be
	// tracked separately to be unregistered later.
	if err := session.Register(127, value.MustParseOID("1.3.6.1.4.1.45996")); err == nil {
		t.Fatal("Register succeeded twice on one session")
	}

	served = m.serve(t, 1, session.ID())
	if err := session.Unregister(127, value.MustParseOID("1.3.6.1.4.1.45995")); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	unregisterReq := <-served
	unregister, ok := unregisterReq.Packet.(*pdu.Unregister)
	if !ok {
		t.Fatalf("packet has type %T, want *pdu.Unregister", unregisterReq.Packet)
	}
	if unregister.Timeout.Priority != 127 {
		t.Fatalf("unregister priority = %d, want 127", unregister.Timeout.Priority)
	}
}

// A master agent that refuses a registration must surface as an error, with
// the code it sent (RFC 2741 6.2.16).
func TestRegisterReportsTheMasterAgentsError(t *testing.T) {
	c, m := newPipeClient(t)
	session := m.openSession(t, c, 1, nil)

	go func() {
		req, ok := <-m.frames
		if !ok {
			return
		}
		m.respond(req, session.ID(), pdu.ErrorDuplicateRegistration)
	}()

	err := session.Register(127, value.MustParseOID("1.3.6.1.4.1.45995"))
	var e pdu.Error
	if !errors.As(err, &e) || e != pdu.ErrorDuplicateRegistration {
		t.Fatalf("Register error = %v, want ErrorDuplicateRegistration", err)
	}
}

// The identifiers of the request being handled are exposed through the
// context, which is the only way a handler can correlate the phases of a set
// transaction (RFC 2741 6.1: one transaction id per management request).
func TestHandlerContextCarriesTheRequestIdentifiers(t *testing.T) {
	c, m := newPipeClient(t)

	seen := make(chan requestIDs, 1)

	handler := &contextHandler{seen: seen}
	session := m.openSession(t, c, 3, handler)

	m.send(masterRequest(pdu.TypeGet, session.ID(), 55, 66, getOf(value.OID{1, 3, 6, 1})))
	m.recv(t)

	got := <-seen
	want := requestIDs{session: session.ID(), transaction: 55, packet: 66}
	if got != want {
		t.Fatalf("context identifiers = %+v, want %+v", got, want)
	}
}

type requestIDs struct{ session, transaction, packet uint32 }

type contextHandler struct {
	testHandler
	seen chan requestIDs
}

func (h *contextHandler) Get(ctx context.Context, oid value.OID) (value.OID, pdu.VariableType, any, error) {
	h.seen <- requestIDs{SessionID(ctx), TransactionID(ctx), PacketID(ctx)}
	return oid, pdu.VariableTypeInteger, int32(1), nil
}

// WithErrorHandler routes background errors - the ones with no caller to
// return them to - into a callback.
func TestWithErrorHandlerReceivesBackgroundErrors(t *testing.T) {
	received := make(chan error, 4)
	c, m := newPipeClient(t, WithErrorHandler(func(err error) { received <- err }))
	session := m.openSession(t, c, 1, nil)

	// A payload that cannot be parsed is reported in the background as well as
	// answered with parseError.
	payload := []byte{100, 0, 0, 0}
	header := &pdu.Header{
		Version:       pdu.Version,
		Type:          pdu.TypeGet,
		SessionID:     session.ID(),
		TransactionID: 1,
		PacketID:      1,
		PayloadLength: uint32(len(payload)),
	}
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	m.sendRaw(append(headerBytes, payload...))

	select {
	case err := <-received:
		if err == nil {
			t.Fatal("error handler was called with nil")
		}
	case <-time.After(frameTimeout):
		t.Fatal("error handler was never called")
	}
}

// A Handler can return a value whose Go type does not match the syntax it
// declared. That fails at encoding time, on the client's own goroutine, where
// there is no caller to return an error to - and a master agent left without
// any reply would sit on the request until it timed the session out. RFC 2741
// 7.2.3 makes genErr the answer for a processing failure.
func TestUnencodableResponseFallsBackToGenErr(t *testing.T) {
	c, m := newPipeClient(t)

	handler := &testHandler{
		get: func(oid value.OID) (value.OID, pdu.VariableType, any, error) {
			// An Integer varbind whose value is a string cannot be encoded.
			return oid, pdu.VariableTypeInteger, "not an int32", nil
		},
	}
	session := m.openSession(t, c, 1, handler)

	m.send(masterRequest(pdu.TypeGet, session.ID(), 1, 1, getOf(value.OID{1, 3, 6, 1})))

	frame := m.recv(t)
	response := responseFrom(t, frame)
	if response.Error != pdu.ErrorGenErr {
		t.Fatalf("response error = %v, want ErrorGenErr", response.Error)
	}
	if frame.Header.PacketID != 1 || frame.Header.TransactionID != 1 {
		t.Fatalf("fallback response addressed to transaction %d packet %d, want 1/1",
			frame.Header.TransactionID, frame.Header.PacketID)
	}
	if len(response.Variables) != 0 {
		t.Fatalf("fallback response carries %d varbinds, want none", len(response.Variables))
	}
}
