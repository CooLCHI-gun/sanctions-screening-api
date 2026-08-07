package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// LLM cascade must be OFF by default and only enabled per-tenant explicitly.
func TestTenant_LLMCascadeDefaultOff(t *testing.T) {
	_, ts, _ := tenantTestEnv(t, nil)
	ctx := t.Context()
	tenant, err := ts.CreateTenant(ctx, "PrivacyFirst", "sk-privacy", "full")
	if err != nil {
		t.Fatal(err)
	}
	if tenant.LLMCascadeEnabled {
		t.Fatal("LLM cascade must default to OFF (privacy)")
	}
	// Round-trip through store lookup
	fetched, err := ts.LookupByAPIKey(ctx, "sk-privacy")
	if err != nil {
		t.Fatal(err)
	}
	if fetched.LLMCascadeEnabled {
		t.Fatal("lookup should show llm_cascade_enabled=false")
	}
}

func TestTenant_LLMCascadeToggle(t *testing.T) {
	_, ts, _ := tenantTestEnv(t, nil)
	ctx := t.Context()
	tenant, err := ts.CreateTenant(ctx, "OptIn", "sk-optin", "full")
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.SetLLMCascade(ctx, tenant.ID, true); err != nil {
		t.Fatal(err)
	}
	fetched, err := ts.LookupByAPIKey(ctx, "sk-optin")
	if err != nil {
		t.Fatal(err)
	}
	if !fetched.LLMCascadeEnabled {
		t.Fatal("expected llm_cascade_enabled=true after opt-in")
	}
}

func TestTenant_ToggleLLMCascadeEndpoint(t *testing.T) {
	_, _, router := tenantTestEnv(t, nil)
	// Create tenant via admin
	req := httptest.NewRequest("POST", "/tenants", strings.NewReader(`{"name":"X","tier":"full"}`))
	req.Header.Set("X-Admin-Key", "test-admin-key")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var created struct {
		TenantID string `json:"tenant_id"`
	}
	jsonDecode(t, rec.Body.Bytes(), &created)

	// Toggle on
	req = httptest.NewRequest("PUT", "/tenants/"+created.TenantID+"/llm-cascade", strings.NewReader(`{"enabled":true}`))
	req.Header.Set("X-Admin-Key", "test-admin-key")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle on: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Toggle off
	req = httptest.NewRequest("PUT", "/tenants/"+created.TenantID+"/llm-cascade", strings.NewReader(`{"enabled":false}`))
	req.Header.Set("X-Admin-Key", "test-admin-key")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle off: expected 200, got %d", rec.Code)
	}
}
