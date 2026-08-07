package review

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Status of a review case.
type Status string

const (
	// StatusPending means the case awaits human review.
	StatusPending Status = "pending"
	// StatusApproved means the review cleared the match (release transaction).
	StatusApproved Status = "approved"
	// StatusRejected means the review confirmed the match (block transaction).
	StatusRejected Status = "rejected"
	// StatusFalsePositive means the review determined the match was incorrect.
	StatusFalsePositive Status = "false_positive"
)

// Case is one item in the review queue: a screening that produced a high-risk
// or LLM-uncertain candidate.
type Case struct {
	ID           int64   `json:"id"`
	TenantID     string  `json:"tenant_id"`
	RequestID    string  `json:"request_id"`
	QueryName    string  `json:"query_name"`
	Candidate    string  `json:"candidate"`
	ListName     string  `json:"list_name"`
	Confidence   float64 `json:"confidence_score"`
	LLMReasoning string  `json:"llm_reasoning,omitempty"`
	// LLM metadata for auditability: which model + which prompt version.
	ModelName     string `json:"model_name,omitempty"`
	PromptVersion string `json:"prompt_version,omitempty"`
	// RiskFlags marks machine-detected concerns (e.g. prompt-injection-suspect).
	RiskFlags []string   `json:"risk_flags,omitempty"`
	Status    Status     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	DecidedAt *time.Time `json:"decided_at,omitempty"`
	DecidedBy string     `json:"decided_by,omitempty"`
}

// Store is the review queue store.
type Store interface {
	// Create inserts a pending case.
	Create(ctx context.Context, c Case) (int64, error)
	// List returns cases for a tenant, filtered by status, newest first.
	List(ctx context.Context, tenantID string, status Status, limit int) ([]Case, error)
	// Get returns one case by ID (tenant-scoped).
	Get(ctx context.Context, tenantID string, id int64) (*Case, error)
	// Decide sets the status of a pending case and records who decided.
	Decide(ctx context.Context, tenantID string, id int64, status Status, decidedBy string) error
	// Close releases the store.
	Close() error
}

// SQLiteStore is a review queue backed by SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) the review DB at path and ensures schema.
func NewSQLiteStore(ctx context.Context, path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("review: open db: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("review: pragma: %w", err)
	}
	s := &SQLiteStore{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	// Base schema (without metadata columns) so new DBs get a clean table.
	if _, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS review_cases (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	tenant_id   TEXT NOT NULL,
	request_id  TEXT NOT NULL,
	query_name  TEXT NOT NULL,
	candidate   TEXT NOT NULL,
	list_name   TEXT NOT NULL,
	confidence  REAL NOT NULL,
	llm_reasoning TEXT,
	status      TEXT NOT NULL DEFAULT 'pending',
	created_at  DATETIME NOT NULL,
	decided_at  DATETIME,
	decided_by  TEXT
);
CREATE INDEX IF NOT EXISTS idx_review_tenant_status ON review_cases(tenant_id, status, id DESC);
`); err != nil {
		return fmt.Errorf("review: migrate: %w", err)
	}
	// Additive migrations: each ALTER is idempotent — a duplicate-column error
	// means the column already exists (old DB upgraded, or table recreated).
	for _, stmt := range []string{
		`ALTER TABLE review_cases ADD COLUMN model_name TEXT`,
		`ALTER TABLE review_cases ADD COLUMN prompt_version TEXT`,
		`ALTER TABLE review_cases ADD COLUMN risk_flags TEXT`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil && !isDuplicateColumn(err) {
			return fmt.Errorf("review: migrate: %w", err)
		}
	}
	return nil
}

// isDuplicateColumn detects SQLite's "duplicate column name" error so additive
// migrations can run idempotently on both fresh and upgraded databases.
func isDuplicateColumn(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column name")
}

func (s *SQLiteStore) Create(ctx context.Context, c Case) (int64, error) {
	if c.Status == "" {
		c.Status = StatusPending
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	flags := strings.Join(c.RiskFlags, ",")
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO review_cases (tenant_id, request_id, query_name, candidate, list_name,
		   confidence, llm_reasoning, model_name, prompt_version, risk_flags, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.TenantID, c.RequestID, c.QueryName, c.Candidate, c.ListName,
		c.Confidence, c.LLMReasoning, c.ModelName, c.PromptVersion, flags, string(c.Status), c.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("review: create: %w", err)
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) List(ctx context.Context, tenantID string, status Status, limit int) ([]Case, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT id, tenant_id, request_id, query_name, candidate, list_name,
	          confidence, COALESCE(llm_reasoning,''), COALESCE(model_name,''),
	          COALESCE(prompt_version,''), COALESCE(risk_flags,''), status, created_at,
	          COALESCE(decided_at,''), COALESCE(decided_by,'')
	      FROM review_cases WHERE tenant_id = ?`
	var args []any = []any{tenantID}
	if status != "" {
		q += ` AND status = ?`
		args = append(args, string(status))
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("review: list: %w", err)
	}
	defer rows.Close()
	var out []Case
	for rows.Next() {
		var (
			c     Case
			ts    string
			dat   sql.NullString
			db    sql.NullString
			flags string
		)
		if err := rows.Scan(&c.ID, &c.TenantID, &c.RequestID, &c.QueryName, &c.Candidate,
			&c.ListName, &c.Confidence, &c.LLMReasoning, &c.ModelName, &c.PromptVersion,
			&flags, &c.Status, &ts, &dat, &db); err != nil {
			return nil, fmt.Errorf("review: scan: %w", err)
		}
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			c.CreatedAt = parsed
		}
		if dat.Valid {
			if parsed, err := time.Parse(time.RFC3339, dat.String); err == nil {
				c.DecidedAt = &parsed
			}
		}
		c.DecidedBy = db.String
		if flags != "" {
			c.RiskFlags = strings.Split(flags, ",")
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) Get(ctx context.Context, tenantID string, id int64) (*Case, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, request_id, query_name, candidate, list_name,
		        confidence, COALESCE(llm_reasoning,''), COALESCE(model_name,''),
		        COALESCE(prompt_version,''), COALESCE(risk_flags,''), status, created_at,
		        COALESCE(decided_at,''), COALESCE(decided_by,'')
		 FROM review_cases WHERE tenant_id = ? AND id = ?`, tenantID, id)
	var (
		c     Case
		ts    string
		dat   sql.NullString
		db    sql.NullString
		flags string
	)
	if err := row.Scan(&c.ID, &c.TenantID, &c.RequestID, &c.QueryName, &c.Candidate,
		&c.ListName, &c.Confidence, &c.LLMReasoning, &c.ModelName, &c.PromptVersion,
		&flags, &c.Status, &ts, &dat, &db); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("review: get: %w", err)
	}
	if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
		c.CreatedAt = parsed
	}
	if dat.Valid {
		if parsed, err := time.Parse(time.RFC3339, dat.String); err == nil {
			c.DecidedAt = &parsed
		}
	}
	c.DecidedBy = db.String
	if flags != "" {
		c.RiskFlags = strings.Split(flags, ",")
	}
	return &c, nil
}

func (s *SQLiteStore) Decide(ctx context.Context, tenantID string, id int64, status Status, decidedBy string) error {
	if status == "" || status == StatusPending {
		return fmt.Errorf("review: cannot set status to %q", status)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE review_cases SET status = ?, decided_at = ?, decided_by = ? WHERE tenant_id = ? AND id = ? AND status = 'pending'`,
		string(status), now, decidedBy, tenantID, id)
	if err != nil {
		return fmt.Errorf("review: decide: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows // not pending or not found
	}
	return nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }
