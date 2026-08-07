package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimiterAllowsWithinBurst(t *testing.T) {
	rl := newRateLimiter(10, 5)
	allowed := 0
	for i := 0; i < 5; i++ {
		if rl.allow("tenant:t1") {
			allowed++
		}
	}
	if allowed != 5 {
		t.Fatalf("expected 5 allowed within burst, got %d", allowed)
	}
	// 6th should be denied (burst exhausted).
	if rl.allow("tenant:t1") {
		t.Fatal("expected 6th call denied")
	}
}

func TestRateLimiterPerKeyIsolation(t *testing.T) {
	rl := newRateLimiter(10, 2)
	// Exhaust t1.
	rl.allow("tenant:t1")
	rl.allow("tenant:t1")
	if rl.allow("tenant:t1") {
		t.Fatal("t1 should be exhausted")
	}
	// t2 unaffected.
	if !rl.allow("tenant:t2") {
		t.Fatal("t2 should be allowed")
	}
}

func TestRateLimitMiddlewareDisabledWhenRateZero(t *testing.T) {
	h := testHandler()
	router := testRouter(h) // rate 0 by default → disabled

	req := httptest.NewRequest("GET", "/health", nil)
	for i := 0; i < 10; i++ {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 when rate limiting disabled, got %d", rec.Code)
		}
	}
}

func TestRateLimitMiddlewareRejectsOverLimit(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rl := RateLimitMiddleware(1, 2, inner) // 1 token/sec, burst 2

	// First two pass (burst), third is limited.
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		rl.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	rl.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
}
