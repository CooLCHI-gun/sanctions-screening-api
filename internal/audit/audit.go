package audit

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// EventType enumerates the kinds of audit events recorded.
type EventType string

const (
	// EventScreeningRequested records that a screening was initiated.
	EventScreeningRequested EventType = "screening_requested"
	// EventScreeningCompleted records the screening outcome.
	EventScreeningCompleted EventType = "screening_completed"
	// EventProviderError records a provider failure that did not abort screening.
	EventProviderError EventType = "provider_error"
	// EventCaseDecision records a manual review decision on a pending case.
	EventCaseDecision EventType = "case_decision"
	// EventCascadeCall records one LLM cascade invocation (cost tracking +
	// per-tenant monthly cap).
	EventCascadeCall EventType = "cascade_call"
)

// Event is one append-only audit record.
type Event struct {
	ID          int64     `json:"id"`
	TenantID    string    `json:"tenant_id"`
	RequestID   string    `json:"request_id,omitempty"`
	EventType   EventType `json:"event_type"`
	PayloadJSON string    `json:"payload_json"`
	CreatedAt   time.Time `json:"created_at"`
}

// Store is an append-only audit log. Records can be inserted and queried but
// never updated or deleted by this package (compliance-friendly).
type Store interface {
	// Record appends one event. Returns the assigned ID.
	Record(ctx context.Context, e Event) (int64, error)
	// Query returns events matching the optional filters, newest first, limited to limit.
	Query(ctx context.Context, tenantID, requestID string, eventType EventType, limit int) ([]Event, error)
	// Count returns the total number of events (optionally filtered by tenant).
	Count(ctx context.Context, tenantID string) (int64, error)
	// CountSince returns the number of events of a type for a tenant since t
	// (used for per-tenant monthly cascade caps).
	CountSince(ctx context.Context, tenantID string, eventType EventType, since time.Time) (int64, error)
	// Ping verifies the underlying DB is reachable.
	Ping(ctx context.Context) error
	// Close releases the underlying store.
	Close() error
}

// SQLiteStore is an append-only audit store backed by SQLite in WAL mode.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) the audit DB at path and ensures schema.
func NewSQLiteStore(ctx context.Context, path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("audit: open db: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("audit: pragma: %w", err)
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
CREATE TABLE IF NOT EXISTS audit_events (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	tenant_id   TEXT NOT NULL,
	request_id  TEXT,
	event_type  TEXT NOT NULL,
	payload_json TEXT NOT NULL DEFAULT '{}',
	created_at  DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_tenant ON audit_events(tenant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_request ON audit_events(request_id);
`)
	if err != nil {
		return fmt.Errorf("audit: migrate: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Record(ctx context.Context, e Event) (int64, error) {
	if e.EventType == "" {
		e.EventType = EventScreeningCompleted
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_events (tenant_id, request_id, event_type, payload_json, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		e.TenantID, e.RequestID, string(e.EventType), e.PayloadJSON,
		e.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("audit: record: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("audit: lastid: %w", err)
	}
	return id, nil
}

func (s *SQLiteStore) Query(ctx context.Context, tenantID, requestID string, eventType EventType, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT id, tenant_id, request_id, event_type, payload_json, created_at FROM audit_events WHERE 1=1`
	var args []any
	if tenantID != "" {
		q += ` AND tenant_id = ?`
		args = append(args, tenantID)
	}
	if requestID != "" {
		q += ` AND request_id = ?`
		args = append(args, requestID)
	}
	if eventType != "" {
		q += ` AND event_type = ?`
		args = append(args, string(eventType))
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("audit: query: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var (
			e  Event
			ts string
		)
		if err := rows.Scan(&e.ID, &e.TenantID, &e.RequestID, &e.EventType, &e.PayloadJSON, &ts); err != nil {
			return nil, fmt.Errorf("audit: scan: %w", err)
		}
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			e.CreatedAt = parsed
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) Count(ctx context.Context, tenantID string) (int64, error) {
	q := `SELECT COUNT(*) FROM audit_events`
	var args []any
	if tenantID != "" {
		q += ` WHERE tenant_id = ?`
		args = append(args, tenantID)
	}
	var n int64
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("audit: count: %w", err)
	}
	return n, nil
}

// CountSince returns the number of events of a type for a tenant since t.
func (s *SQLiteStore) CountSince(ctx context.Context, tenantID string, eventType EventType, since time.Time) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_events WHERE tenant_id = ? AND event_type = ? AND created_at >= ?`,
		tenantID, string(eventType), since.UTC().Format(time.RFC3339)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("audit: count_since: %w", err)
	}
	return n, nil
}

// Ping verifies the SQLite DB is reachable.
func (s *SQLiteStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *SQLiteStore) Close() error { return s.db.Close() }
