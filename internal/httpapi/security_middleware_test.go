package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestAdminMiddleware_NoKeyConfigured(t *testing.T) {
	mw := AdminMiddleware("", okHandler())
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when admin disabled, got %d", rec.Code)
	}
}

func TestAdminMiddleware_WrongKey(t *testing.T) {
	mw := AdminMiddleware("secret", okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-Admin-Key", "wrong")
	mw.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong admin key, got %d", rec.Code)
	}
}

func TestAdminMiddleware_ValidKey(t *testing.T) {
	mw := AdminMiddleware("secret", okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-Admin-Key", "secret")
	mw.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid admin key, got %d", rec.Code)
	}
}

func TestRapidProxyMiddleware_DisabledWhenEmpty(t *testing.T) {
	mw := RapidProxyMiddleware("", okHandler())
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when proxy secret disabled, got %d", rec.Code)
	}
}

func TestRapidProxyMiddleware_RejectsMissingHeader(t *testing.T) {
	mw := RapidProxyMiddleware("proxy-secret", okHandler())
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing proxy secret, got %d", rec.Code)
	}
}

func TestRapidProxyMiddleware_RejectsWrongSecret(t *testing.T) {
	mw := RapidProxyMiddleware("proxy-secret", okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-RapidAPI-Proxy-Secret", "wrong")
	mw.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong proxy secret, got %d", rec.Code)
	}
}

func TestRapidProxyMiddleware_AcceptsValidSecret(t *testing.T) {
	mw := RapidProxyMiddleware("proxy-secret", okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-RapidAPI-Proxy-Secret", "proxy-secret")
	mw.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid proxy secret, got %d", rec.Code)
	}
}

// Tenant creation now requires admin key end-to-end.
func TestTenantFlow_AdminKeyRequired(t *testing.T) {
	_, _, router := tenantTestEnv(t, nil)
	req := httptest.NewRequest("POST", "/tenants", strings.NewReader(`{"name":"No Key","tier":"full"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without admin key, got %d", rec.Code)
	}
}
