package tenant

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tenants.db")
	s, err := NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndLookup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	ten, err := s.CreateTenant(ctx, "Acme Crypto", "sk-acme-123", TierFull)
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if ten.ID == "" || ten.Name != "Acme Crypto" || ten.Tier != TierFull {
		t.Fatalf("unexpected tenant: %+v", ten)
	}
	if ten.APIKeyHash == "sk-acme-123" {
		t.Fatal("APIKeyHash must be hashed, not raw")
	}

	got, err := s.LookupByAPIKey(ctx, "sk-acme-123")
	if err != nil {
		t.Fatalf("LookupByAPIKey: %v", err)
	}
	if got.ID != ten.ID || got.Tier != TierFull {
		t.Fatalf("lookup mismatch: %+v", got)
	}
}

func TestLookupWrongKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.LookupByAPIKey(ctx, "sk-wrong"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestInvalidTierRejected(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.CreateTenant(ctx, "Bad", "sk-x", Tier("superuser")); err != ErrInvalidTier {
		t.Fatalf("expected ErrInvalidTier, got %v", err)
	}
}

func TestHashAPIKeyStableAndOneWay(t *testing.T) {
	h1 := HashAPIKey("secret")
	h2 := HashAPIKey("secret")
	if h1 != h2 {
		t.Fatal("hash must be deterministic")
	}
	if len(h1) != 64 {
		t.Fatalf("sha256 hex must be 64 chars, got %d", len(h1))
	}
	if HashAPIKey("secret") == HashAPIKey("secret2") {
		t.Fatal("different keys must hash differently")
	}
}

func TestListTenants(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.CreateTenant(ctx, "A", "key-a", TierScreeningOnly); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTenant(ctx, "B", "key-b", TierFull); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListTenants(ctx)
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 tenants, got %d", len(all))
	}
}
