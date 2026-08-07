package watchlist

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "watchlist.db")
	s, err := NewSQLiteStore(context.Background(), path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUploadAndActive(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	v1, err := s.UploadList(ctx, "t1", "exchange_blacklist", []Entry{
		{Name: "Bad Actor One", Type: "person", Country: "US", Tags: []string{"scam"}},
		{Name: "Fake Exchange Ltd", Type: "entity", Country: "HK", Identifiers: []string{"wallet:0xabc"}},
	})
	if err != nil {
		t.Fatalf("UploadList v1: %v", err)
	}
	if v1 != 1 {
		t.Fatalf("expected version 1, got %d", v1)
	}

	entries, err := s.ActiveEntries(ctx, "t1")
	if err != nil {
		t.Fatalf("ActiveEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 active entries, got %d", len(entries))
	}

	// Upload v2 with one entry — old version must be deactivated.
	v2, err := s.UploadList(ctx, "t1", "exchange_blacklist", []Entry{
		{Name: "Only New Name", Type: "person"},
	})
	if err != nil {
		t.Fatalf("UploadList v2: %v", err)
	}
	if v2 != 2 {
		t.Fatalf("expected version 2, got %d", v2)
	}
	entries, _ = s.ActiveEntries(ctx, "t1")
	if len(entries) != 1 || entries[0].Name != "Only New Name" {
		t.Fatalf("expected only new entry active, got %+v", entries)
	}
}

func TestListNames(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, _ = s.UploadList(ctx, "t1", "list_a", []Entry{{Name: "X"}})
	_, _ = s.UploadList(ctx, "t1", "list_a", []Entry{{Name: "Y"}})
	_, _ = s.UploadList(ctx, "t1", "list_b", []Entry{{Name: "Z"}})

	names, err := s.ListNames(ctx, "t1")
	if err != nil {
		t.Fatalf("ListNames: %v", err)
	}
	if names["list_a"] != 2 || names["list_b"] != 1 {
		t.Fatalf("unexpected versions: %+v", names)
	}
	// Other tenant isolated.
	other, _ := s.ListNames(ctx, "t2")
	if len(other) != 0 {
		t.Fatalf("tenant isolation broken: %+v", other)
	}
}

func TestParseCSV(t *testing.T) {
	csvData := `entity_id,name,type,country,dob,identifiers,tags
e1,John Smith,person,US,1980-01-01,passport:123|wallet:0x1,pep|scam
e2,Evil Corp,entity,CN,,,exchange_ban
`
	entries, err := ParseCSV(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Name != "John Smith" || entries[0].Type != "person" {
		t.Fatalf("entry 0 wrong: %+v", entries[0])
	}
	if len(entries[0].Identifiers) != 2 {
		t.Fatalf("expected 2 identifiers, got %+v", entries[0].Identifiers)
	}
	if len(entries[1].Tags) != 1 || entries[1].Tags[0] != "exchange_ban" {
		t.Fatalf("entry 1 tags wrong: %+v", entries[1].Tags)
	}
}

func TestParseCSVMissingNameColumn(t *testing.T) {
	if _, err := ParseCSV(strings.NewReader("foo,bar\n1,2\n")); err == nil {
		t.Fatal("expected error for missing name column")
	}
}
