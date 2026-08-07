package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/CooLCHI-gun/sanctions-screening-api/internal/audit"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/llmcascade"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/models"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/review"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/screening"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/watchlist"
)

// ServiceInfo holds metadata about the running service instance.
// Used by operational endpoints (/version, /metadata, /ready).
type ServiceInfo struct {
	Version        string
	ProviderName   string
	RecordCount    int
	ListVersion    string
	DataSource     string
	DataVersion    string
	SourcesCovered []string
}

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	screeningSvc screening.Service
	logger       *slog.Logger
	info         ServiceInfo
	audit        audit.Store
	watchlists   watchlist.Store
	reviews      review.Store
}

// NewHandler creates a new Handler with the given dependencies.
// Stores may be nil (legacy mode).
func NewHandler(svc screening.Service, logger *slog.Logger, info ServiceInfo,
	auditStore audit.Store, watchlistStore watchlist.Store, reviewStore review.Store) *Handler {
	return &Handler{
		screeningSvc: svc,
		logger:       logger,
		info:         info,
		audit:        auditStore,
		watchlists:   watchlistStore,
		reviews:      reviewStore,
	}
}

// Health responds with a simple health check JSON payload.
// Only GET is allowed.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "sanctions-screening-api",
	})
}

// Ready responds with 200 if the service is ready to accept requests.
// Includes record count so consumers can verify data is loaded. In tenant
// mode the audit DB is pinged; a failing DB yields 503 so orchestrators do
// not route traffic to a degraded instance.
// Only GET is allowed.
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
		return
	}

	status := "ready"
	if h.info.RecordCount == 0 {
		status = "no-data"
	}

	if h.audit != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := h.audit.Ping(ctx); err != nil {
			h.logger.Error("ready: audit db unhealthy", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status":       "degraded",
				"service":      "sanctions-screening-api",
				"record_count": h.info.RecordCount,
				"db":           "unreachable",
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":       status,
		"service":      "sanctions-screening-api",
		"record_count": h.info.RecordCount,
		"data_version": h.info.DataVersion,
		"sources":      h.info.SourcesCovered,
	})
}

// Version responds with service version metadata.
// Only GET is allowed.
func (h *Handler) Version(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"service": "sanctions-screening-api",
		"version": h.info.Version,
	})
}

// Metadata responds with operational metadata about the running instance:
// provider, data source, list version, and record count.
// Only GET is allowed.
func (h *Handler) Metadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"service":      "sanctions-screening-api",
		"version":      h.info.Version,
		"provider":     h.info.ProviderName,
		"data_source":  h.info.DataSource,
		"list_version": h.info.ListVersion,
		"record_count": h.info.RecordCount,
	})
}

// Screen accepts a screening request, validates it, and returns results.
// Only POST is allowed.
func (h *Handler) Screen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
		return
	}

	var req models.ScreeningRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidBody, "invalid request body")
		return
	}

	// Check for trailing data: only io.EOF at the end is valid.
	var trailing struct{}
	if err := dec.Decode(&trailing); err != io.EOF {
		writeError(w, http.StatusBadRequest, ErrCodeTrailingData, "request body contains trailing data")
		return
	}

	// Normalize and validate query_name
	req.QueryName = strings.TrimSpace(req.QueryName)
	if req.QueryName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeMissingField, "query_name is required")
		return
	}

	// Validate threshold range if provided
	if req.Threshold != 0 && (req.Threshold < 0 || req.Threshold > 1) {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidThreshold, "threshold must be between 0.0 and 1.0")
		return
	}

	// Normalize and validate entity_type if provided
	if req.EntityType != "" {
		req.EntityType = models.EntityType(strings.ToLower(strings.TrimSpace(string(req.EntityType))))
		if !isValidEntityType(req.EntityType) {
			writeError(w, http.StatusBadRequest, ErrCodeInvalidEntityType, "invalid entity_type: must be individual, organization, vessel, or aircraft")
			return
		}
	}

	result, err := h.screeningSvc.Screen(r.Context(), req)
	if err != nil {
		h.logger.Error("screening failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "screening failed")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// Body size limits (protect against DoS via huge payloads).
const (
	maxJSONBodyBytes = 1 << 20 // 1 MiB for JSON endpoints
	maxCSVBodyBytes  = 5 << 20 // 5 MiB for watchlist CSV upload
)

// ScreenTenant is the tenant-aware screening handler. It loads the tenant's
// watchlists, screens against them plus global records, writes an audit event,
// and (for high-risk/uncertain results) creates a review case.
// Only POST is allowed.
func (h *Handler) ScreenTenant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
		return
	}
	t, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "tenant required")
		return
	}

	// Body size limit: reject oversized JSON payloads early (DoS guard).
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)

	var req models.ScreeningRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, ErrCodeTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, ErrCodeInvalidBody, "invalid request body")
		return
	}
	var trailing struct{}
	if err := dec.Decode(&trailing); err != io.EOF {
		writeError(w, http.StatusBadRequest, ErrCodeTrailingData, "request body contains trailing data")
		return
	}
	req.QueryName = strings.TrimSpace(req.QueryName)
	if req.QueryName == "" {
		writeError(w, http.StatusBadRequest, ErrCodeMissingField, "query_name is required")
		return
	}
	if req.Threshold != 0 && (req.Threshold < 0 || req.Threshold > 1) {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidThreshold, "threshold must be between 0.0 and 1.0")
		return
	}
	if req.EntityType != "" {
		req.EntityType = models.EntityType(strings.ToLower(strings.TrimSpace(string(req.EntityType))))
		if !isValidEntityType(req.EntityType) {
			writeError(w, http.StatusBadRequest, ErrCodeInvalidEntityType, "invalid entity_type: must be individual, organization, vessel, or aircraft")
			return
		}
	}

	// Load tenant watchlists (if any configured).
	var wlEntries []screening.WatchlistEntry
	if h.watchlists != nil {
		entries, err := h.watchlists.ActiveEntries(r.Context(), t.ID)
		if err != nil {
			h.logger.Error("load watchlists failed", "error", err)
			writeError(w, http.StatusInternalServerError, ErrCodeInternal, "watchlist load failed")
			return
		}
		for _, e := range entries {
			wlEntries = append(wlEntries, screening.WatchlistEntry{
				Name:        e.Name,
				Type:        e.Type,
				Country:     e.Country,
				DOB:         e.DOB,
				ListName:    e.ListName,
				Version:     e.Version,
				Identifiers: e.Identifiers,
				Tags:        e.Tags,
			})
		}
	}

	result, err := h.screeningSvc.ScreenForTenant(r.Context(), req, t.ID, wlEntries, t.LLMCascadeEnabled)
	if err != nil {
		h.logger.Error("screening failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "screening failed")
		return
	}

	// Audit the screening.
	if h.audit != nil {
		payload, _ := json.Marshal(map[string]any{
			"query":           req.QueryName,
			"status":          result.Status,
			"total_matches":   result.TotalMatches,
			"candidate_names": candidateNames(result.Candidates),
			"llm_escalated":   hasLLMVerification(result.Candidates),
		})
		_, _ = h.audit.Record(r.Context(), audit.Event{
			TenantID:    t.ID,
			RequestID:   result.RequestID,
			EventType:   audit.EventScreeningCompleted,
			PayloadJSON: string(payload),
		})
	}

	// High-risk results create a review case (pending).
	if result.Status == models.StatusPending && h.reviews != nil {
		for _, c := range result.Candidates {
			if c.Sensitivity == models.SensitivitySanctionsRelated && c.ConfidenceScore >= 0.9 {
				reason := ""
				if c.LLMVerification != nil {
					reason = c.LLMVerification.Reasoning
				}
				// LLM output is untrusted text: sanitize + flag injection suspects.
				reason = llmcascade.SanitizeReasoning(reason)
				flags := llmcascade.DetectRiskFlags(reason)
				_, _ = h.reviews.Create(r.Context(), review.Case{
					TenantID:      t.ID,
					RequestID:     result.RequestID,
					QueryName:     req.QueryName,
					Candidate:     c.EntityName,
					ListName:      c.ListName,
					Confidence:    c.ConfidenceScore,
					LLMReasoning:  reason,
					ModelName:     llmcascade.CurrentModel(),
					PromptVersion: llmcascade.PromptVersion,
					RiskFlags:     flags,
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, result)
}

func candidateNames(cs []models.ScreeningCandidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.EntityName)
	}
	return out
}

func hasLLMVerification(cs []models.ScreeningCandidate) bool {
	for _, c := range cs {
		if c.LLMVerification != nil {
			return true
		}
	}
	return false
}

func isValidEntityType(et models.EntityType) bool {
	for _, t := range []models.EntityType{
		models.EntityTypeIndividual,
		models.EntityTypeOrganization,
		models.EntityTypeVessel,
		models.EntityTypeAircraft,
	} {
		if et == t {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Error response shape: {"error": {"code": "...", "message": "..."}}
// writeError sends a consistent JSON error payload with a machine-readable code.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]map[string]string{
		"error": {"code": code, "message": msg},
	})
}
