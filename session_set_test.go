// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package agentx

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/LJS360d/go-agentx/pdu"
	"github.com/LJS360d/go-agentx/value"
)

// twoPhaseHandler records which phase touched it, so a test can assert that
// nothing is applied before commitSet.
type twoPhaseHandler struct {
	tested    []string
	committed []string
	undone    []string
	testErr   error
	commitErr error
}

func (h *twoPhaseHandler) Get(context.Context, value.OID) (value.OID, pdu.VariableType, any, error) {
	return nil, pdu.VariableTypeNull, nil, nil
}

func (h *twoPhaseHandler) GetNext(context.Context, value.OID, bool, value.OID) (value.OID, pdu.VariableType, any, error) {
	return nil, pdu.VariableTypeNull, nil, nil
}

func (h *twoPhaseHandler) Set(_ context.Context, oid value.OID, _ pdu.VariableType, _ any) error {
	panic("Set must not be called on a handler implementing SetHandler")
}

func (h *twoPhaseHandler) TestSet(_ context.Context, req SetRequest) error {
	if h.testErr != nil {
		return h.testErr
	}
	h.tested = append(h.tested, req.OID.String())
	return nil
}

func (h *twoPhaseHandler) CommitSet(_ context.Context, reqs []SetRequest) error {
	if h.commitErr != nil {
		return h.commitErr
	}
	for _, req := range reqs {
		h.committed = append(h.committed, req.OID.String())
	}
	return nil
}

func (h *twoPhaseHandler) UndoSet(_ context.Context, reqs []SetRequest) error {
	for _, req := range reqs {
		h.undone = append(h.undone, req.OID.String())
	}
	return nil
}

// legacyHandler implements only Handler and must keep the old behaviour. It
// deliberately does not embed twoPhaseHandler: embedding would drag in the
// SetHandler methods and put it on the two-phase path.
type legacyHandler struct {
	set []string
}

func (h *legacyHandler) Get(context.Context, value.OID) (value.OID, pdu.VariableType, any, error) {
	return nil, pdu.VariableTypeNull, nil, nil
}

func (h *legacyHandler) GetNext(context.Context, value.OID, bool, value.OID) (value.OID, pdu.VariableType, any, error) {
	return nil, pdu.VariableTypeNull, nil, nil
}

func (h *legacyHandler) Set(_ context.Context, oid value.OID, _ pdu.VariableType, _ any) error {
	h.set = append(h.set, oid.String())
	return nil
}

func newTestSession(handler Handler) *Session {
	return &Session{
		client:  &Client{logger: slog.New(slog.NewTextHandler(io.Discard, nil))},
		handler: handler,
		pending: make(map[uint32][]SetRequest),
	}
}

func setPacket(txID uint32, packet pdu.Packet, oids ...string) *pdu.HeaderPacket {
	var variables pdu.Variables
	for _, oid := range oids {
		variables.Add(value.MustParseOID(oid), pdu.VariableTypeInteger, int32(1))
	}

	switch p := packet.(type) {
	case *pdu.TestSet:
		p.Variables = variables
	case *pdu.CommitSet:
		p.Variables = variables
	case *pdu.UndoSet:
		p.Variables = variables
	case *pdu.CleanupSet:
		p.Variables = variables
	}

	return &pdu.HeaderPacket{
		Header: &pdu.Header{Type: packet.Type(), TransactionID: txID},
		Packet: packet,
	}
}

func responseOf(hp *pdu.HeaderPacket) *pdu.Response {
	return hp.Packet.(*pdu.Response)
}

const (
	oidA = "1.3.6.1.4.1.45995.1"
	oidB = "1.3.6.1.4.1.45995.2"
)

func TestTwoPhaseSetCommits(t *testing.T) {
	h := &twoPhaseHandler{}
	s := newTestSession(h)

	res := responseOf(s.handle(setPacket(7, &pdu.TestSet{}, oidA, oidB)))
	if res.Error != pdu.ErrorNone {
		t.Fatalf("testSet: got error %v, want none", res.Error)
	}
	// RFC 2741 6.2.6: the testSet response VarBindList must be empty.
	if len(res.Variables) != 0 {
		t.Fatalf("testSet: response carries %d varbinds, want 0", len(res.Variables))
	}
	if len(h.tested) != 2 {
		t.Fatalf("testSet: tested %v, want both OIDs", h.tested)
	}
	if len(h.committed) != 0 {
		t.Fatalf("testSet applied %v, must not touch anything before commit", h.committed)
	}

	res = responseOf(s.handle(setPacket(7, &pdu.CommitSet{})))
	if res.Error != pdu.ErrorNone {
		t.Fatalf("commitSet: got error %v, want none", res.Error)
	}
	if len(h.committed) != 2 {
		t.Fatalf("commitSet: committed %v, want both OIDs", h.committed)
	}

	s.handle(setPacket(7, &pdu.CleanupSet{}))
	if len(s.pending) != 0 {
		t.Fatalf("cleanupSet left %d transactions pending", len(s.pending))
	}
}

func TestTwoPhaseSetFailedTestDoesNotCommit(t *testing.T) {
	h := &twoPhaseHandler{testErr: fmt.Errorf("read-only: %w", pdu.ErrorNotWritable)}
	s := newTestSession(h)

	res := responseOf(s.handle(setPacket(9, &pdu.TestSet{}, oidA)))
	if res.Error != pdu.ErrorNotWritable {
		t.Fatalf("testSet: got error %v, want ErrorNotWritable", res.Error)
	}
	if res.Index != 1 {
		t.Fatalf("testSet: got index %d, want 1", res.Index)
	}

	s.handle(setPacket(9, &pdu.CommitSet{}))
	if len(h.committed) != 0 {
		t.Fatalf("commitSet applied %v after a failed testSet", h.committed)
	}
}

func TestTwoPhaseSetUndoAfterFailedCommit(t *testing.T) {
	h := &twoPhaseHandler{commitErr: fmt.Errorf("hardware busy")}
	s := newTestSession(h)

	s.handle(setPacket(11, &pdu.TestSet{}, oidA))

	res := responseOf(s.handle(setPacket(11, &pdu.CommitSet{})))
	if res.Error != pdu.ErrorCommitFailed {
		t.Fatalf("commitSet: got error %v, want ErrorCommitFailed", res.Error)
	}

	// The bindings must survive commitSet so undoSet can still see them.
	res = responseOf(s.handle(setPacket(11, &pdu.UndoSet{})))
	if res.Error != pdu.ErrorNone {
		t.Fatalf("undoSet: got error %v, want none", res.Error)
	}
	if len(h.undone) != 1 || h.undone[0] != value.MustParseOID(oidA).String() {
		t.Fatalf("undoSet: undone %v, want %s", h.undone, oidA)
	}
}

func TestLegacyHandlerStillSetsDuringTest(t *testing.T) {
	h := &legacyHandler{}
	s := newTestSession(h)

	res := responseOf(s.handle(setPacket(13, &pdu.TestSet{}, oidA)))
	if res.Error != pdu.ErrorNone {
		t.Fatalf("testSet: got error %v, want none", res.Error)
	}
	if len(h.set) != 1 {
		t.Fatalf("legacy handler: Set called %d times, want 1", len(h.set))
	}
	if len(s.pending) != 0 {
		t.Fatalf("legacy handler: %d transactions pending, want 0", len(s.pending))
	}
}
