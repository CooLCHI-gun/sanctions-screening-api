package httpapi

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CooLCHI-gun/sanctions-screening-api/internal/audit"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/review"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/screening"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/tenant"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/watchlist"
)

var cryptoRand = rand.Read

// ReviewHandler exposes the review queue (cases) API.
type ReviewHandler struct {
	store  review.Store
	audit  audit.Store
	logger *slog.Logger
}

// ListCases returns pending/decided cases for the authenticated tenant.
// GET /cases?status=pending&limit=50
func (h *ReviewHandler) ListCases(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
		return
	}
	t, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "tenant required")
		return
	}
	status := review.Status(r.URL.Query().Get("status"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	cases, err := h.store.List(r.Context(), t.ID, status, limit)
	if err != nil {
		h.logger.Error("list cases failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "list cases failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cases": cases})
}

// GetCase returns one case by ID.
// GET /cases/{id}
func (h *ReviewHandler) GetCase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
		return
	}
	t, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "tenant required")
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/cases/"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidBody, "invalid case id")
		return
	}
	c, err := h.store.Get(r.Context(), t.ID, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, ErrCodeNotFound, "case not found")
			return
		}
		h.logger.Error("get case failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "get case failed")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// GetOrDecide routes /cases/{id} by method: GET → GetCase, POST → DecideCase.
func (h *ReviewHandler) GetOrDecide(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.GetCase(w, r)
		return
	}
	if r.Method == http.MethodPost {
		h.DecideCase(w, r)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
}

// DecisionRequest is the body of POST /cases/{id}/decision.
type DecisionRequest struct {
	Decision string `json:"decision"`
}

// DecideCase records a manual review decision on a pending case.
// POST /cases/{id}/decision  {"decision": "approved"|"rejected"|"false_positive"}
func (h *ReviewHandler) DecideCase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
		return
	}
	t, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "tenant required")
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/cases/"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidBody, "invalid case id")
		return
	}
	var req DecisionRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidBody, "invalid request body")
		return
	}
	status := review.Status(req.Decision)
	if status != review.StatusApproved && status != review.StatusRejected && status != review.StatusFalsePositive {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidBody, "decision must be approved, rejected, or false_positive")
		return
	}
	decidedBy := sanitizeReviewer(r.Header.Get("X-Reviewer"))
	if decidedBy == "" {
		decidedBy = "api"
	}
	if err := h.store.Decide(r.Context(), t.ID, id, status, decidedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusConflict, ErrCodeConflict, "case not pending or not found")
			return
		}
		h.logger.Error("decide case failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "decide failed")
		return
	}
	// Audit the decision.
	if h.audit != nil {
		payload, _ := json.Marshal(map[string]any{"case_id": id, "decision": status, "decided_by": decidedBy})
		_, _ = h.audit.Record(r.Context(), audit.Event{
			TenantID:    t.ID,
			RequestID:   "",
			EventType:   audit.EventCaseDecision,
			PayloadJSON: string(payload),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "case_id": id, "decision": status})
}

// WatchlistHandler exposes tenant watchlist upload + list endpoints.
type WatchlistHandler struct {
	store  watchlist.Store
	logger *slog.Logger
}

// UploadWatchlist replaces a tenant list version from a CSV body.
// POST /watchlists/{list_name}  (body: CSV with header entity_id,name,type,country,dob,identifiers,tags)
func (h *WatchlistHandler) UploadWatchlist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
		return
	}
	t, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "tenant required")
		return
	}
	listName := strings.TrimPrefix(r.URL.Path, "/watchlists/")
	listName = strings.TrimSuffix(listName, "/upload")
	if listName == "" || strings.ContainsAny(listName, "/ 	") {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidBody, "invalid list name")
		return
	}
	// Body size limit: reject oversized CSV uploads (DoS guard).
	r.Body = http.MaxBytesReader(w, r.Body, maxCSVBodyBytes)
	entries, err := watchlist.ParseCSV(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidBody, err.Error())
		return
	}
	version, err := h.store.UploadList(r.Context(), t.ID, listName, entries)
	if err != nil {
		h.logger.Error("upload watchlist failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "upload failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "list_name": listName, "version": version, "entries": len(entries)})
}

// ListWatchlists returns list names + current versions for the tenant.
// GET /watchlists
func (h *WatchlistHandler) ListWatchlists(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
		return
	}
	t, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "tenant required")
		return
	}
	names, err := h.store.ListNames(r.Context(), t.ID)
	if err != nil {
		h.logger.Error("list watchlists failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "list failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"watchlists": names})
}

// AuditHandler exposes audit log queries (full tier only).
type AuditHandler struct {
	store  audit.Store
	logger *slog.Logger
}

// ListAudit returns audit events for the authenticated tenant, newest first.
// GET /audit?event_type=screening_completed&limit=50
func (h *AuditHandler) ListAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
		return
	}
	t, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "tenant required")
		return
	}
	et := audit.EventType(r.URL.Query().Get("event_type"))
	requestID := r.URL.Query().Get("request_id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	events, err := h.store.Query(r.Context(), t.ID, requestID, et, limit)
	if err != nil {
		h.logger.Error("list audit failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "audit query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

// TenantHandler exposes tenant management (admin).
type TenantHandler struct {
	store  tenant.Store
	logger *slog.Logger
}

// CreateTenantRequest is the body of POST /tenants.
type CreateTenantRequest struct {
	Name   string `json:"name"`
	Tier   string `json:"tier"`
	APIKey string `json:"api_key"` // optional: if empty, server generates one
}

// CreateTenant creates a tenant and returns its API key (shown once).
// POST /tenants  {"name": "...", "tier": "screening_only"|"full"}
func (h *TenantHandler) CreateTenant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
		return
	}
	var req CreateTenantRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidBody, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, ErrCodeMissingField, "name is required")
		return
	}
	tier := tenant.Tier(strings.ToLower(strings.TrimSpace(req.Tier)))
	if !tenant.ValidateTier(tier) {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidBody, "tier must be screening_only or full")
		return
	}
	rawKey := req.APIKey
	if rawKey == "" {
		rawKey = "sk-" + randomToken(32)
	}
	t, err := h.store.CreateTenant(r.Context(), req.Name, rawKey, tier)
	if err != nil {
		if errors.Is(err, tenant.ErrInvalidTier) {
			writeError(w, http.StatusBadRequest, ErrCodeInvalidBody, "invalid tier")
			return
		}
		h.logger.Error("create tenant failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "create failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": t.ID,
		"name":      t.Name,
		"tier":      t.Tier,
		"api_key":   rawKey, // shown once — client must store it
	})
}

// ListTenants returns all tenants (hash omitted by model).
// GET /tenants
func (h *TenantHandler) ListTenants(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
		return
	}
	tenants, err := h.store.ListTenants(r.Context())
	if err != nil {
		h.logger.Error("list tenants failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "list failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenants": tenants})
}

// CreateOrList routes /tenants by method: GET → ListTenants, POST → CreateTenant.
func (h *TenantHandler) CreateOrList(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.ListTenants(w, r)
		return
	}
	if r.Method == http.MethodPost {
		h.CreateTenant(w, r)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
}

// SetLLMCascadeRequest is the body of PUT /tenants/{id}/llm-cascade.
type SetLLMCascadeRequest struct {
	Enabled bool `json:"enabled"`
}

// ToggleLLMCascade enables/disables the optional LLM cascade for a tenant.
// PUT /tenants/{id}/llm-cascade  {"enabled": true}
// Default is OFF: PII must not leave our infra unless the customer opts in.
func (h *TenantHandler) ToggleLLMCascade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/tenants/")
	id = strings.TrimSuffix(id, "/llm-cascade")
	if id == "" || strings.ContainsAny(id, "/ 	") {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidBody, "invalid tenant id")
		return
	}
	var req SetLLMCascadeRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidBody, "invalid request body")
		return
	}
	if err := h.store.SetLLMCascade(r.Context(), id, req.Enabled); err != nil {
		if errors.Is(err, tenant.ErrNotFound) {
			writeError(w, http.StatusNotFound, ErrCodeNotFound, "tenant not found")
			return
		}
		h.logger.Error("set llm cascade failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenant_id": id, "llm_cascade_enabled": req.Enabled})
}

// reviewerPattern is the allowed format for X-Reviewer: email or username.
// Stored headers are untrusted input — restrict charset + length to prevent
// header/UI injection (stored XSS in review UIs).
var reviewerPattern = regexp.MustCompile(`^[A-Za-z0-9._%+\-@]{1,128}$`)

// sanitizeReviewer validates an X-Reviewer header value. Returns "" for
// invalid values (caller falls back to "api").
func sanitizeReviewer(v string) string {
	v = strings.TrimSpace(v)
	if !reviewerPattern.MatchString(v) {
		return ""
	}
	return v
}

// randomToken returns a cryptographically random hex token of n bytes.
func randomToken(n int) string {
	const hexdigits = "0123456789abcdef"
	b := make([]byte, n)
	if _, err := cryptoRand(b); err == nil {
		// bytes are 0-255; map into hex chars
		out := make([]byte, n)
		for i, v := range b {
			out[i] = hexdigits[v%16]
		}
		return string(out)
	}
	// Fallback (non-crypto) — should never be hit on normal systems.
	for i := range b {
		b[i] = hexdigits[time.Now().UnixNano()%16]
	}
	return string(b)
}

// ensure unused import of screening stays for future wiring.
var _ = screening.Service(nil)

// keep io imported for trailing-data checks in future handlers.
var _ = io.EOF
