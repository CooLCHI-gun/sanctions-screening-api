package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/CooLCHI-gun/sanctions-screening-api/internal/audit"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/review"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/screening"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/tenant"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/watchlist"
)

// tenantTestEnv wires in-memory-file stores and a legacy-mode-free router.
func tenantTestEnv(t *testing.T, records []screening.WatchlistEntry) (*Handler, *tenant.SQLiteStore, http.Handler) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()

	ts, err := tenant.NewSQLiteStore(ctx, filepath.Join(dir, "tenants.db"))
	if err != nil {
		t.Fatalf("tenant store: %v", err)
	}
	as, err := audit.NewSQLiteStore(ctx, filepath.Join(dir, "audit.db"))
	if err != nil {
		t.Fatalf("audit store: %v", err)
	}
	ws, err := watchlist.NewSQLiteStore(ctx, filepath.Join(dir, "watchlists.db"))
	if err != nil {
		t.Fatalf("watchlist store: %v", err)
	}
	rs, err := review.NewSQLiteStore(ctx, filepath.Join(dir, "review.db"))
	if err != nil {
		t.Fatalf("review store: %v", err)
	}
	t.Cleanup(func() {
		ts.Close()
		as.Close()
		ws.Close()
		rs.Close()
	})

	svc := screening.NewService()
	svc.LoadRecords(nil)
	h := NewHandler(svc, slog.Default(), ServiceInfo{Version: "test", ProviderName: "local", RecordCount: 0}, as, ws, rs)
	router := NewRouter(h, RouterOptions{
		TenantStore:    ts,
		AuditStore:     as,
		ReviewStore:    rs,
		WatchlistStore: ws,
		Logger:         slog.Default(),
		AdminKey:       "test-admin-key",
	})
	return h, ts, router
}

func TestTenantFlow_EndToEnd(t *testing.T) {
	_, ts, router := tenantTestEnv(t, nil)
	ctx := context.Background()

	// 1. Create a tenant (admin key required).
	createBody := `{"name":"Acme Crypto","tier":"full"}`
	req := httptest.NewRequest("POST", "/tenants", strings.NewReader(createBody))
	req.Header.Set("X-Admin-Key", "test-admin-key")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create tenant: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		TenantID string `json:"tenant_id"`
		APIKey   string `json:"api_key"`
		Tier     string `json:"tier"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.APIKey == "" || created.TenantID == "" {
		t.Fatal("expected api_key + tenant_id")
	}

	// 2. Lookup by API key works (store-level).
	if _, err := ts.LookupByAPIKey(ctx, created.APIKey); err != nil {
		t.Fatalf("lookup by api key: %v", err)
	}

	// 3. Upload a watchlist with the tenant key.
	csvBody := "entity_id,name,type,country,dob,identifiers,tags\nwl1,SUSPECT A,person,HK,,,scam\n"
	req = httptest.NewRequest("POST", "/watchlists/internal_risk", strings.NewReader(csvBody))
	req.Header.Set("X-API-Key", created.APIKey)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload watchlist: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// 4. Screen against the watchlist (exact match → high confidence → pending).
	screenBody := `{"query_name":"SUSPECT A"}`
	req = httptest.NewRequest("POST", "/screen", strings.NewReader(screenBody))
	req.Header.Set("X-API-Key", created.APIKey)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("screen: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var screenResp struct {
		Status       string `json:"status"`
		TotalMatches int    `json:"total_matches"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &screenResp); err != nil {
		t.Fatal(err)
	}
	if screenResp.TotalMatches != 1 {
		t.Fatalf("expected 1 match, got %d", screenResp.TotalMatches)
	}

	// 5. List pending cases (watchlist exact match → pending review).
	req = httptest.NewRequest("GET", "/cases?status=pending", nil)
	req.Header.Set("X-API-Key", created.APIKey)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list cases: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var casesResp struct {
		Cases []struct {
			ID        int64  `json:"id"`
			QueryName string `json:"query_name"`
			Candidate string `json:"candidate"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &casesResp); err != nil {
		t.Fatal(err)
	}
	if len(casesResp.Cases) != 1 {
		t.Fatalf("expected 1 pending case, got %d", len(casesResp.Cases))
	}

	// 6. Decide the case.
	caseID := casesResp.Cases[0].ID
	decBody := `{"decision":"false_positive"}`
	req = httptest.NewRequest("POST", "/cases/"+strconv.FormatInt(caseID, 10), strings.NewReader(decBody))
	req.Header.Set("X-API-Key", created.APIKey)
	req.Header.Set("X-Reviewer", "ops@acme")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("decide case: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// 7. Audit log should contain the screening + decision events.
	req = httptest.NewRequest("GET", "/audit", nil)
	req.Header.Set("X-API-Key", created.APIKey)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit: expected 200, got %d", rec.Code)
	}
	var auditResp struct {
		Events []struct {
			EventType string `json:"event_type"`
		} `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &auditResp); err != nil {
		t.Fatal(err)
	}
	if len(auditResp.Events) < 2 {
		t.Fatalf("expected ≥2 audit events, got %d", len(auditResp.Events))
	}
}

func TestTenantFlow_WrongKeyRejected(t *testing.T) {
	_, _, router := tenantTestEnv(t, nil)

	req := httptest.NewRequest("POST", "/screen", strings.NewReader(`{"query_name":"X"}`))
	req.Header.Set("X-API-Key", "sk-wrong-key")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong key, got %d", rec.Code)
	}
}

func TestTenantFlow_NoKeyRejected(t *testing.T) {
	_, _, router := tenantTestEnv(t, nil)

	req := httptest.NewRequest("POST", "/screen", strings.NewReader(`{"query_name":"X"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing key, got %d", rec.Code)
	}
}

func TestTenantFlow_ScreeningOnlyTierForbiddenOnCases(t *testing.T) {
	_, ts, router := tenantTestEnv(t, nil)
	ctx := context.Background()

	if _, err := ts.CreateTenant(ctx, "readonly", "sk-readonly", tenant.TierScreeningOnly); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/cases", nil)
	req.Header.Set("X-API-Key", "sk-readonly")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for screening_only tier on /cases, got %d", rec.Code)
	}
}
