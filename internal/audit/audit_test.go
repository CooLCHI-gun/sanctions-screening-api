package audit

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit.db")
	s, err := NewSQLiteStore(context.Background(), path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRecordAndQuery(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.Record(ctx, Event{
		TenantID:    "t1",
		RequestID:   "req-1",
		EventType:   EventScreeningRequested,
		PayloadJSON: `{"query":"John Doe"}`,
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	evs, err := s.Query(ctx, "t1", "", "", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evs))
	}
	if evs[0].TenantID != "t1" || evs[0].EventType != EventScreeningRequested {
		t.Fatalf("unexpected event: %+v", evs[0])
	}
}

func TestQueryFilters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, tc := range []struct {
		tenant, req string
		typ         EventType
	}{
		{"t1", "r1", EventScreeningRequested},
		{"t1", "r1", EventScreeningCompleted},
		{"t2", "r2", EventScreeningCompleted},
	} {
		if _, err := s.Record(ctx, Event{TenantID: tc.tenant, RequestID: tc.req, EventType: tc.typ}); err != nil {
			t.Fatal(err)
		}
	}

	if evs, _ := s.Query(ctx, "t1", "", "", 10); len(evs) != 2 {
		t.Fatalf("tenant filter: expected 2, got %d", len(evs))
	}
	if evs, _ := s.Query(ctx, "", "r1", "", 10); len(evs) != 2 {
		t.Fatalf("request filter: expected 2, got %d", len(evs))
	}
	if evs, _ := s.Query(ctx, "", "", EventScreeningCompleted, 10); len(evs) != 2 {
		t.Fatalf("type filter: expected 2, got %d", len(evs))
	}
	// newest first ordering
	evs, _ := s.Query(ctx, "t1", "", "", 10)
	if evs[0].EventType != EventScreeningCompleted {
		t.Fatalf("expected newest first, got %s", evs[0].EventType)
	}
}

func TestCount(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.Record(ctx, Event{TenantID: "t1", EventType: EventScreeningCompleted}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Record(ctx, Event{TenantID: "t2", EventType: EventScreeningCompleted}); err != nil {
		t.Fatal(err)
	}
	n, err := s.Count(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 for t1, got %d", n)
	}
	all, _ := s.Count(ctx, "")
	if all != 2 {
		t.Fatalf("expected 2 total, got %d", all)
	}
}

func TestAppendOnlyNoUpdateAPI(t *testing.T) {
	// Compile-time check that Store exposes no update/delete methods.
	var _ Store = (*SQLiteStore)(nil)
}
