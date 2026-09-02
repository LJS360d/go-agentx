// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package agentx

import (
	"context"

	"github.com/LJS360d/go-agentx/pdu"
	"github.com/LJS360d/go-agentx/value"
)

// Handler defines an interface for a handler of events that
// might occure during a session.
type Handler interface {
	Get(context.Context, value.OID) (value.OID, pdu.VariableType, any, error)
	GetNext(context.Context, value.OID, bool, value.OID) (value.OID, pdu.VariableType, any, error)
	Set(context.Context, value.OID, pdu.VariableType, any) error
}

// SetRequest is one variable binding of a set transaction.
type SetRequest struct {
	OID   value.OID
	Type  pdu.VariableType
	Value any
}

// SetHandler is an optional extension of Handler that opts a session into the
// full RFC 2741 set protocol. A handler that implements it gets:
//
//	TestSet   validate only; must not apply the change
//	CommitSet apply every variable binding that passed TestSet
//	UndoSet   roll back a commit the master agent has abandoned
//
// The session accumulates the bindings that passed TestSet for the duration of
// the transaction and hands the whole slice to CommitSet and UndoSet, so a
// handler does not need to track transaction state of its own. Handler.Set is
// never called on a handler implementing SetHandler.
//
// A handler that implements only Handler keeps the legacy behaviour: Set runs
// during the testSet phase and commitSet is a no-op, which means the change is
// applied before the master agent has decided to commit.
type SetHandler interface {
	TestSet(ctx context.Context, req SetRequest) error
	CommitSet(ctx context.Context, reqs []SetRequest) error
	UndoSet(ctx context.Context, reqs []SetRequest) error
}

type (
	sessionIDKey     struct{}
	transactionIDKey struct{}
	packetIDKey      struct{}
)

func SessionID(ctx context.Context) uint32 {
	value, _ := ctx.Value(sessionIDKey{}).(uint32)
	return value
}

func withSessionID(ctx context.Context, value uint32) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, value)
}

func TransactionID(ctx context.Context) uint32 {
	value, _ := ctx.Value(transactionIDKey{}).(uint32)
	return value
}

func withTransactionID(ctx context.Context, value uint32) context.Context {
	return context.WithValue(ctx, transactionIDKey{}, value)
}

func PacketID(ctx context.Context) uint32 {
	value, _ := ctx.Value(packetIDKey{}).(uint32)
	return value
}

func withPacketID(ctx context.Context, value uint32) context.Context {
	return context.WithValue(ctx, packetIDKey{}, value)
}
