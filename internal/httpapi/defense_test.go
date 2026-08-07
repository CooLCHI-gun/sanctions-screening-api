package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSanitizeReviewer_Valid(t *testing.T) {
	cases := []string{"ops@acme.com", "john.doe", "user+tag@example.com", "A1_b-c@x.io"}
	for _, c := range cases {
		if got := sanitizeReviewer(c); got != c {
			t.Errorf("sanitizeReviewer(%q) = %q, want %q", c, got, c)
		}
	}
}

func TestSanitizeReviewer_Invalid(t *testing.T) {
	cases := []string{
		"", " ", "a b", "a\nb", "a<script>", strings.Repeat("a", 129), "héllo", "a=b;c",
	}
	for _, c := range cases {
		if got := sanitizeReviewer(c); got != "" {
			t.Errorf("sanitizeReviewer(%q) = %q, want empty", c, got)
		}
	}
}

func TestBodyLimit_ScreenRejectsOversized(t *testing.T) {
	_, _, router := tenantTestEnv(t, nil)
	// Create tenant via admin + use it
	req := httptest.NewRequest("POST", "/tenants", strings.NewReader(`{"name":"X","tier":"full"}`))
	req.Header.Set("X-Admin-Key", "test-admin-key")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var created struct {
		APIKey string `json:"api_key"`
	}
	jsonDecode(t, rec.Body.Bytes(), &created)

	big := `{"query_name":"` + strings.Repeat("x", maxJSONBodyBytes+100) + `"}`
	req = httptest.NewRequest("POST", "/screen", strings.NewReader(big))
	req.Header.Set("X-API-Key", created.APIKey)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized body, got %d", rec.Code)
	}
}

func TestWatchlistUpload_RejectsFormulaInjection(t *testing.T) {
	_, _, router := tenantTestEnv(t, nil)
	req := httptest.NewRequest("POST", "/tenants", strings.NewReader(`{"name":"X","tier":"full"}`))
	req.Header.Set("X-Admin-Key", "test-admin-key")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var created struct {
		APIKey string `json:"api_key"`
	}
	jsonDecode(t, rec.Body.Bytes(), &created)

	// =cmd formula in name field
	csv := "entity_id,name,type,country\nwl1,=HYPERLINK(\"http://evil.com\"),person,US\n"
	req = httptest.NewRequest("POST", "/watchlists/evil", strings.NewReader(csv))
	req.Header.Set("X-API-Key", created.APIKey)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for formula injection, got %d", rec.Code)
	}
}

func jsonDecode(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode: %v", err)
	}
}
