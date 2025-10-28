// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package agentx

import (
	"context"

	"github.com/posteo/go-agentx/pdu"
	"github.com/posteo/go-agentx/value"
)

// ListHandler is a helper that takes a list of oids and implements
// a default behaviour for that list.
type ListHandler struct {
	oids  []value.OID
	items map[string]*ListItem
}

// Set updates the value for the provided oid.
func (l *ListHandler) Set(ctx context.Context, oid value.OID, t pdu.VariableType, value any) error {
	if l.items == nil {
		return nil
	}

	item, ok := l.items[oid.String()]
	if !ok {
		return nil
	}

	item.Type = t
	item.Value = value
	return nil
}

// Add adds a list item for the provided oid and returns it.
func (l *ListHandler) Add(oid string) *ListItem {
	if l.items == nil {
		l.items = make(map[string]*ListItem)
	}

	parsedOID := value.MustParseOID(oid)
	l.oids = append(l.oids, parsedOID)
	value.SortOIDs(l.oids)
	item := &ListItem{}
	l.items[oid] = item
	return item
}

// Get tries to find the provided oid and returns the corresponding value.
func (l *ListHandler) Get(ctx context.Context, oid value.OID) (value.OID, pdu.VariableType, any, error) {
	if l.items == nil {
		return nil, pdu.VariableTypeNoSuchObject, nil, nil
	}

	item, ok := l.items[oid.String()]
	if ok {
		return oid, item.Type, item.Value, nil
	}
	return nil, pdu.VariableTypeNoSuchObject, nil, nil
}

// GetNext tries to find the value that follows the provided oid and returns it.
func (l *ListHandler) GetNext(ctx context.Context, from value.OID, includeFrom bool, to value.OID) (value.OID, pdu.VariableType, any, error) {
	if l.items == nil {
		return nil, pdu.VariableTypeNoSuchObject, nil, nil
	}

	for _, oid := range l.oids {
		if oidWithin(oid, from, includeFrom, to) {
			return l.Get(ctx, oid)
		}
	}

	return nil, pdu.VariableTypeNoSuchObject, nil, nil
}

func oidWithin(oid value.OID, from value.OID, includeFrom bool, to value.OID) bool {
	fromCompare := value.CompareOIDs(from, oid)
	toCompare := value.CompareOIDs(to, oid)

	return (fromCompare == -1 || (fromCompare == 0 && includeFrom)) && (toCompare == 1)
}
