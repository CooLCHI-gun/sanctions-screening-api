package tenant

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no CGO)
)

// SQLiteStore is a tenant store backed by SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) the tenant DB at path and ensures schema.
func NewSQLiteStore(ctx context.Context, path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("tenant: open db: %w", err)
	}
	// WAL improves concurrent read/write on a single VPS instance.
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("tenant: pragma: %w", err)
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
CREATE TABLE IF NOT EXISTS tenants (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL,
	api_key_hash TEXT NOT NULL UNIQUE,
	tier        TEXT NOT NULL,
	created_at  DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tenants_hash ON tenants(api_key_hash);
`)
	if err != nil {
		return fmt.Errorf("tenant: migrate: %w", err)
	}
	// Additive migration: llm_cascade_enabled (privacy opt-in, default 0/off).
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE tenants ADD COLUMN llm_cascade_enabled INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
		return fmt.Errorf("tenant: migrate: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CreateTenant(ctx context.Context, name, rawAPIKey string, tier Tier) (*Tenant, error) {
	if !ValidateTier(tier) {
		return nil, ErrInvalidTier
	}
	t := &Tenant{
		ID:         newID(),
		Name:       name,
		APIKeyHash: HashAPIKey(rawAPIKey),
		Tier:       tier,
		CreatedAt:  time.Now().UTC(),
	}
	// LLM cascade is DEFAULT OFF — PII must not leave our infra unless the
	// customer explicitly opts in.
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, api_key_hash, tier, llm_cascade_enabled, created_at) VALUES (?, ?, ?, ?, 0, ?)`,
		t.ID, t.Name, t.APIKeyHash, string(t.Tier), t.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("tenant: create: %w", err)
	}
	return t, nil
}

func (s *SQLiteStore) LookupByAPIKey(ctx context.Context, rawAPIKey string) (*Tenant, error) {
	hash := HashAPIKey(rawAPIKey)
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, api_key_hash, tier, llm_cascade_enabled, created_at FROM tenants WHERE api_key_hash = ?`, hash)
	return scanTenant(row)
}

func (s *SQLiteStore) ListTenants(ctx context.Context) ([]Tenant, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, api_key_hash, tier, llm_cascade_enabled, created_at FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("tenant: list: %w", err)
	}
	defer rows.Close()
	var out []Tenant
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// SetLLMCascade enables or disables the optional LLM cascade for a tenant.
func (s *SQLiteStore) SetLLMCascade(ctx context.Context, tenantID string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE tenants SET llm_cascade_enabled = ? WHERE id = ?`, v, tenantID)
	if err != nil {
		return fmt.Errorf("tenant: set_llm_cascade: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTenant(row rowScanner) (*Tenant, error) {
	var (
		t    Tenant
		ts   string
		tier string
		llm  int
	)
	if err := row.Scan(&t.ID, &t.Name, &t.APIKeyHash, &tier, &llm, &ts); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("tenant: scan: %w", err)
	}
	t.Tier = Tier(tier)
	t.LLMCascadeEnabled = llm == 1
	parsed, err := time.Parse(time.RFC3339, ts)
	if err == nil {
		t.CreatedAt = parsed
	}
	return &t, nil
}

// newID returns a short unique ID for a tenant (timestamp + random suffix).
func newID() string {
	return fmt.Sprintf("t_%d_%06x", time.Now().Unix(), time.Now().UnixNano()&0xffffff)
}
