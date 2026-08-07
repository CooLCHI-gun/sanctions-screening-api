package watchlist

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Entry is one row of a tenant's custom watchlist.
type Entry struct {
	ID          int64    `json:"id"`
	TenantID    string   `json:"tenant_id"`
	ListName    string   `json:"list_name"`
	Version     int      `json:"version"`
	EntityID    string   `json:"entity_id,omitempty"`
	Name        string   `json:"name"`
	Type        string   `json:"type,omitempty"` // person | entity
	Country     string   `json:"country,omitempty"`
	DOB         string   `json:"dob,omitempty"`
	Identifiers []string `json:"identifiers,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Active      bool     `json:"active"`
}

// Store manages tenant watchlists.
type Store interface {
	// UploadList replaces a list version: inserts new rows under version+1 and
	// deactivates the previous version's rows. Returns the new version number.
	UploadList(ctx context.Context, tenantID, listName string, entries []Entry) (int, error)
	// ActiveEntries returns all active entries for a tenant (all lists).
	ActiveEntries(ctx context.Context, tenantID string) ([]Entry, error)
	// ListNames returns list names + current version for a tenant.
	ListNames(ctx context.Context, tenantID string) (map[string]int, error)
	// Close releases the store.
	Close() error
}

// SQLiteStore is a watchlist store backed by SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) the watchlist DB at path and ensures schema.
func NewSQLiteStore(ctx context.Context, path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("watchlist: open db: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("watchlist: pragma: %w", err)
	}
	s := &SQLiteStore{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS tenant_watchlist_entries (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	tenant_id   TEXT NOT NULL,
	list_name   TEXT NOT NULL,
	version     INTEGER NOT NULL,
	entity_id   TEXT,
	name        TEXT NOT NULL,
	type        TEXT,
	country     TEXT,
	dob         TEXT,
	identifiers TEXT,
	tags        TEXT,
	active      BOOLEAN NOT NULL DEFAULT 1,
	created_at  DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_watchlist_tenant_active ON tenant_watchlist_entries(tenant_id, active);
`)
	if err != nil {
		return fmt.Errorf("watchlist: migrate: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UploadList(ctx context.Context, tenantID, listName string, entries []Entry) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("watchlist: begin: %w", err)
	}
	defer tx.Rollback()

	var maxV int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM tenant_watchlist_entries WHERE tenant_id = ? AND list_name = ?`,
		tenantID, listName).Scan(&maxV); err != nil {
		return 0, fmt.Errorf("watchlist: max version: %w", err)
	}
	newVersion := maxV + 1

	// Deactivate previous version.
	if _, err := tx.ExecContext(ctx,
		`UPDATE tenant_watchlist_entries SET active = 0 WHERE tenant_id = ? AND list_name = ?`,
		tenantID, listName); err != nil {
		return 0, fmt.Errorf("watchlist: deactivate: %w", err)
	}

	now := time.Now().UTC()
	for i := range entries {
		e := &entries[i]
		e.TenantID = tenantID
		e.ListName = listName
		e.Version = newVersion
		e.Active = true

		ids, err := json.Marshal(e.Identifiers)
		if err != nil {
			return 0, fmt.Errorf("watchlist: marshal identifiers: %w", err)
		}
		tags, err := json.Marshal(e.Tags)
		if err != nil {
			return 0, fmt.Errorf("watchlist: marshal tags: %w", err)
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO tenant_watchlist_entries
			 (tenant_id, list_name, version, entity_id, name, type, country, dob, identifiers, tags, active, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
			e.TenantID, e.ListName, e.Version, e.EntityID, e.Name, e.Type,
			e.Country, e.DOB, string(ids), string(tags), now.Format(time.RFC3339)); err != nil {
			return 0, fmt.Errorf("watchlist: insert: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("watchlist: commit: %w", err)
	}
	return newVersion, nil
}

func (s *SQLiteStore) ActiveEntries(ctx context.Context, tenantID string) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, list_name, version, COALESCE(entity_id,''), name,
		        COALESCE(type,''), COALESCE(country,''), COALESCE(dob,''),
		        COALESCE(identifiers,'[]'), COALESCE(tags,'[]'), active
		 FROM tenant_watchlist_entries WHERE tenant_id = ? AND active = 1`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("watchlist: active: %w", err)
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var (
			e   Entry
			ids string
			tg  string
		)
		if err := rows.Scan(&e.ID, &e.TenantID, &e.ListName, &e.Version, &e.EntityID,
			&e.Name, &e.Type, &e.Country, &e.DOB, &ids, &tg, &e.Active); err != nil {
			return nil, fmt.Errorf("watchlist: scan: %w", err)
		}
		_ = json.Unmarshal([]byte(ids), &e.Identifiers)
		_ = json.Unmarshal([]byte(tg), &e.Tags)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ListNames(ctx context.Context, tenantID string) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT list_name, MAX(version) FROM tenant_watchlist_entries WHERE tenant_id = ? GROUP BY list_name`,
		tenantID)
	if err != nil {
		return nil, fmt.Errorf("watchlist: list names: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var n string
		var v int
		if err := rows.Scan(&n, &v); err != nil {
			return nil, err
		}
		out[n] = v
	}
	return out, rows.Err()
}

// isFormulaPrefix reports whether s starts with a spreadsheet formula
// character. These are rejected to prevent CSV formula injection when a
// customer exports a watchlist to Excel/Sheets.
func isFormulaPrefix(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case '=', '+', '-', '@':
		return true
	default:
		return false
	}
}

// truncateForError limits error message content so a rejected row never leaks
// a huge payload into logs/UI.
func truncateForError(s string) string {
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

// ParseCSV parses a watchlist CSV. Expected header:
// entity_id,name,type,country,dob,identifiers,tags
// identifiers and tags are pipe-separated within a cell (|).
func ParseCSV(r io.Reader) ([]Entry, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // allow variable columns
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("watchlist: csv: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("watchlist: csv: need header + at least one row")
	}
	header := rows[0]
	col := func(name string) int {
		for i, h := range header {
			if strings.TrimSpace(strings.ToLower(h)) == name {
				return i
			}
		}
		return -1
	}
	idx := map[string]int{
		"entity_id": col("entity_id"), "name": col("name"), "type": col("type"),
		"country": col("country"), "dob": col("dob"), "identifiers": col("identifiers"),
		"tags": col("tags"),
	}
	if idx["name"] < 0 {
		return nil, fmt.Errorf("watchlist: csv: missing required 'name' column")
	}
	split := func(s string) []string {
		var out []string
		for _, part := range strings.Split(s, "|") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	var entries []Entry
	for _, row := range rows[1:] {
		if len(row) == 0 || strings.TrimSpace(row[0]) == "" {
			continue
		}
		get := func(name string) string {
			i := idx[name]
			if i >= 0 && i < len(row) {
				return strings.TrimSpace(row[i])
			}
			return ""
		}
		name := get("name")
		// CSV formula injection guard: reject cells that start with Excel
		// formula characters (= + - @). Such values are never legitimate
		// entity names and would execute as formulas if a customer opens the
		// watchlist in a spreadsheet.
		for _, field := range []string{name, get("entity_id"), get("country"), get("dob")} {
			if isFormulaPrefix(field) {
				return nil, fmt.Errorf("watchlist: csv: row rejected — field starts with a spreadsheet formula character (= + - @): %q", truncateForError(field))
			}
		}
		entries = append(entries, Entry{
			EntityID:    get("entity_id"),
			Name:        name,
			Type:        get("type"),
			Country:     get("country"),
			DOB:         get("dob"),
			Identifiers: split(get("identifiers")),
			Tags:        split(get("tags")),
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("watchlist: csv: no data rows")
	}
	return entries, nil
}

// ParseVersion parses a version string for API usage.
func ParseVersion(s string) (int, error) {
	return strconv.Atoi(s)
}
