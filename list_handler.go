// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package agentx

import (
	"context"
	"sync"

	"github.com/LJS360d/go-agentx/pdu"
	"github.com/LJS360d/go-agentx/value"
)

// ListHandler is a helper that takes a list of oids and implements
// a default behaviour for that list.
//
// It is safe for concurrent use: the master agent dispatches requests to a
// session from the client's goroutines, so Add and Set can race with Get and
// GetNext unless the list is guarded.
type ListHandler struct {
	mutex sync.RWMutex
	oids  []value.OID
	items map[string]*ListItem
}

// Set updates the value for the provided oid.
func (l *ListHandler) Set(ctx context.Context, oid value.OID, t pdu.VariableType, value any) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

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
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if l.items == nil {
		l.items = make(map[string]*ListItem)
	}

	if item, ok := l.items[oid]; ok {
		return item
	}

	l.oids = append(l.oids, value.MustParseOID(oid))
	value.SortOIDs(l.oids)

	item := &ListItem{}
	l.items[oid] = item
	return item
}

// Get tries to find the provided oid and returns the corresponding value.
func (l *ListHandler) Get(ctx context.Context, oid value.OID) (value.OID, pdu.VariableType, any, error) {
	l.mutex.RLock()
	defer l.mutex.RUnlock()

	return l.get(oid)
}

// get looks an oid up. The caller holds at least the read lock.
func (l *ListHandler) get(oid value.OID) (value.OID, pdu.VariableType, any, error) {
	item, ok := l.items[oid.String()]
	if !ok {
		return nil, pdu.VariableTypeNoSuchObject, nil, nil
	}
	return oid, item.Type, item.Value, nil
}

// GetNext tries to find the value that follows the provided oid and returns it.
func (l *ListHandler) GetNext(ctx context.Context, from value.OID, includeFrom bool, to value.OID) (value.OID, pdu.VariableType, any, error) {
	l.mutex.RLock()
	defer l.mutex.RUnlock()

	for _, oid := range l.oids {
		if oidWithin(oid, from, includeFrom, to) {
			return l.get(oid)
		}
	}

	return nil, pdu.VariableTypeNoSuchObject, nil, nil
}

// oidWithin reports whether oid falls inside the SearchRange [from, to).
//
// RFC 2741 5.2: an empty ending OID is a null Object Identifier and means the
// search range is unbounded above. That case has to be spelled out - an empty
// OID compares as the smallest value, not the largest.
func oidWithin(oid value.OID, from value.OID, includeFrom bool, to value.OID) bool {
	fromCompare := value.CompareOIDs(from, oid)
	if fromCompare > 0 || (fromCompare == 0 && !includeFrom) {
		return false
	}

	if len(to) == 0 {
		return true
	}
	return value.CompareOIDs(to, oid) > 0
}
