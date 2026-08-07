package review

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "review.db")
	s, err := NewSQLiteStore(context.Background(), path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.Create(ctx, Case{
		TenantID:   "t1",
		RequestID:  "req-1",
		QueryName:  "John Smith",
		Candidate:  "JOHN SMITH",
		ListName:   "OFAC SDN",
		Confidence: 0.92,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	cases, err := s.List(ctx, "t1", StatusPending, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(cases) != 1 || cases[0].Status != StatusPending {
		t.Fatalf("unexpected cases: %+v", cases)
	}

	// Tenant isolation
	if cases, _ := s.List(ctx, "t2", "", 10); len(cases) != 0 {
		t.Fatalf("tenant isolation broken: %+v", cases)
	}
}

func TestDecideFlow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.Create(ctx, Case{TenantID: "t1", RequestID: "r", QueryName: "q", Candidate: "c", ListName: "L"})

	// Wrong tenant cannot decide.
	if err := s.Decide(ctx, "t2", id, StatusRejected, "ops"); err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows for wrong tenant, got %v", err)
	}

	if err := s.Decide(ctx, "t1", id, StatusRejected, "ops@acme"); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	got, err := s.Get(ctx, "t1", id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusRejected || got.DecidedBy != "ops@acme" {
		t.Fatalf("decision not persisted: %+v", got)
	}
	if got.DecidedAt == nil {
		t.Fatal("DecidedAt should be set")
	}

	// Cannot decide twice.
	if err := s.Decide(ctx, "t1", id, StatusApproved, "x"); err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows for second decide, got %v", err)
	}
}

func TestListFilterByStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id1, _ := s.Create(ctx, Case{TenantID: "t1", RequestID: "r1", QueryName: "q", Candidate: "c", ListName: "L"})
	id2, _ := s.Create(ctx, Case{TenantID: "t1", RequestID: "r2", QueryName: "q", Candidate: "c", ListName: "L"})
	_ = id1
	_ = s.Decide(ctx, "t1", id2, StatusApproved, "ops")

	pending, _ := s.List(ctx, "t1", StatusPending, 10)
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	approved, _ := s.List(ctx, "t1", StatusApproved, 10)
	if len(approved) != 1 {
		t.Fatalf("expected 1 approved, got %d", len(approved))
	}
}

func TestAutoCreatedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.Create(ctx, Case{TenantID: "t1", RequestID: "r", QueryName: "q", Candidate: "c", ListName: "L"})
	got, _ := s.Get(ctx, "t1", id)
	if got.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be set automatically")
	}
	if got.CreatedAt.After(time.Now().Add(time.Minute)) {
		t.Fatal("CreatedAt in the future")
	}
}
