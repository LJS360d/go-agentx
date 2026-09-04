// Copyright 2018 The agentx authors
// Licensed under the LGPLv3 with static-linking exception.
// See LICENCE file for details.

package agentx

import (
	"context"
	"sync"
	"testing"

	"github.com/LJS360d/go-agentx/pdu"
	"github.com/LJS360d/go-agentx/value"
)

func newListHandler(t *testing.T, oids ...string) *ListHandler {
	t.Helper()

	l := &ListHandler{}
	for i, oid := range oids {
		item := l.Add(oid)
		item.Type = pdu.VariableTypeInteger
		item.Value = int32(i)
	}
	return l
}

func TestListHandlerGet(t *testing.T) {
	l := newListHandler(t, "1.3.6.1.4.1.45995.1", "1.3.6.1.4.1.45995.2")

	oid, vtype, v, err := l.Get(context.Background(), value.MustParseOID("1.3.6.1.4.1.45995.2"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if oid.String() != "1.3.6.1.4.1.45995.2" || vtype != pdu.VariableTypeInteger || v != int32(1) {
		t.Fatalf("Get = %v, %v, %v", oid, vtype, v)
	}

	// RFC 2741 7.2.3.1 (3): an OID that names nothing is noSuchObject, which
	// the session encodes from the nil OID.
	oid, vtype, _, err = l.Get(context.Background(), value.MustParseOID("1.3.6.1.4.1.45995.9"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if oid != nil || vtype != pdu.VariableTypeNoSuchObject {
		t.Fatalf("Get of an unknown OID = %v, %v; want nil, VariableTypeNoSuchObject", oid, vtype)
	}
}

// A zero-valued ListHandler has to answer rather than panic: a session can be
// created with one before anything is added.
func TestListHandlerEmpty(t *testing.T) {
	l := &ListHandler{}

	if _, vtype, _, err := l.Get(context.Background(), value.MustParseOID("1.3.6.1")); err != nil || vtype != pdu.VariableTypeNoSuchObject {
		t.Fatalf("Get on an empty handler = %v, %v", vtype, err)
	}
	if _, _, _, err := l.GetNext(context.Background(), value.MustParseOID("1.3.6.1"), true, nil); err != nil {
		t.Fatalf("GetNext on an empty handler: %v", err)
	}
	if err := l.Set(context.Background(), value.MustParseOID("1.3.6.1"), pdu.VariableTypeInteger, int32(1)); err != nil {
		t.Fatalf("Set on an empty handler: %v", err)
	}
}

// RFC 2741 7.2.3.2: the include flag decides whether the starting OID itself
// is a candidate, and a null ending OID (5.2) means the range is unbounded.
func TestListHandlerGetNextRangeSemantics(t *testing.T) {
	l := newListHandler(t, "1.3.6.1.4.1.45995.1", "1.3.6.1.4.1.45995.2", "1.3.6.1.4.1.45995.3")

	cases := []struct {
		name        string
		from        string
		includeFrom bool
		to          value.OID
		want        string
	}{
		{"exclusive start", "1.3.6.1.4.1.45995.1", false, nil, "1.3.6.1.4.1.45995.2"},
		{"inclusive start", "1.3.6.1.4.1.45995.1", true, nil, "1.3.6.1.4.1.45995.1"},
		{"unbounded end", "1.3.6.1.4.1.45995.2", false, nil, "1.3.6.1.4.1.45995.3"},
		{"empty end oid is unbounded", "1.3.6.1.4.1.45995.2", false, value.OID{}, "1.3.6.1.4.1.45995.3"},
		{"below the subtree", "1.3.6.1", false, nil, "1.3.6.1.4.1.45995.1"},
		{"end excludes the only candidate", "1.3.6.1.4.1.45995.2", false, value.MustParseOID("1.3.6.1.4.1.45995.3"), ""},
		{"past the end of the list", "1.3.6.1.4.1.45995.3", false, nil, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			oid, _, _, err := l.GetNext(context.Background(), value.MustParseOID(c.from), c.includeFrom, c.to)
			if err != nil {
				t.Fatalf("GetNext: %v", err)
			}
			if c.want == "" {
				if oid != nil {
					t.Fatalf("GetNext = %v, want nothing", oid)
				}
				return
			}
			if oid == nil || oid.String() != c.want {
				t.Fatalf("GetNext = %v, want %s", oid, c.want)
			}
		})
	}
}

// Items are returned in lexicographic order regardless of the order they were
// added in - a getNext walk depends on it.
func TestListHandlerKeepsOIDsSorted(t *testing.T) {
	l := newListHandler(t, "1.3.6.1.4.1.45995.10", "1.3.6.1.4.1.45995.2", "1.3.6.1.4.1.45995.1")

	var walked []string
	from := value.MustParseOID("1.3.6.1")
	for {
		oid, _, _, err := l.GetNext(context.Background(), from, false, nil)
		if err != nil {
			t.Fatalf("GetNext: %v", err)
		}
		if oid == nil {
			break
		}
		walked = append(walked, oid.String())
		from = oid
	}

	want := []string{"1.3.6.1.4.1.45995.1", "1.3.6.1.4.1.45995.2", "1.3.6.1.4.1.45995.10"}
	if len(walked) != len(want) {
		t.Fatalf("walk = %v, want %v", walked, want)
	}
	for i := range want {
		if walked[i] != want[i] {
			t.Fatalf("walk = %v, want %v", walked, want)
		}
	}
}

// Adding the same OID twice returns the existing item instead of leaving a
// duplicate in the sorted list, which would make a getNext walk repeat itself.
func TestListHandlerAddIsIdempotent(t *testing.T) {
	l := &ListHandler{}
	first := l.Add("1.3.6.1.4.1.45995.1")
	second := l.Add("1.3.6.1.4.1.45995.1")

	if first != second {
		t.Fatal("Add returned a different item for the same OID")
	}
	if len(l.oids) != 1 {
		t.Fatalf("oids = %v, want a single entry", l.oids)
	}
}

func TestListHandlerSet(t *testing.T) {
	l := newListHandler(t, "1.3.6.1.4.1.45995.1")

	if err := l.Set(context.Background(), value.MustParseOID("1.3.6.1.4.1.45995.1"), pdu.VariableTypeOctetString, "new"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	_, vtype, v, _ := l.Get(context.Background(), value.MustParseOID("1.3.6.1.4.1.45995.1"))
	if vtype != pdu.VariableTypeOctetString || v != "new" {
		t.Fatalf("after Set: %v, %v", vtype, v)
	}

	// Setting an OID the list does not hold is a no-op, not an error.
	if err := l.Set(context.Background(), value.MustParseOID("1.3.6.1.4.1.45995.9"), pdu.VariableTypeInteger, int32(1)); err != nil {
		t.Fatalf("Set of an unknown OID: %v", err)
	}
}

// The master agent dispatches requests from the client's goroutines while the
// owning program may still be updating values, so the list has to be safe for
// concurrent use. Run with -race.
func TestListHandlerConcurrentAccess(t *testing.T) {
	l := newListHandler(t, "1.3.6.1.4.1.45995.1", "1.3.6.1.4.1.45995.2")
	oid := value.MustParseOID("1.3.6.1.4.1.45995.1")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				l.Set(context.Background(), oid, pdu.VariableTypeInteger, int32(j))
				l.Get(context.Background(), oid)
				l.GetNext(context.Background(), value.MustParseOID("1.3.6.1"), false, nil)
				l.Add("1.3.6.1.4.1.45995.3")
			}
		}(i)
	}
	wg.Wait()
}
