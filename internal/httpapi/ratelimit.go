package httpapi

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// rateLimiter implements a per-key token bucket limiter.
type rateLimiter struct {
	mu       sync.Mutex
	rate     float64 // tokens per second
	burst    int
	clients  map[string]*clientBucket
	lastSeen time.Time
}

type clientBucket struct {
	tokens   float64
	last     time.Time
	lastSeen time.Time
}

// newRateLimiter creates a limiter with the given per-second rate and burst size.
func newRateLimiter(rate float64, burst int) *rateLimiter {
	if rate <= 0 {
		rate = 1
	}
	if burst <= 0 {
		burst = 1
	}
	return &rateLimiter{
		rate:    rate,
		burst:   burst,
		clients: make(map[string]*clientBucket),
	}
}

// allow checks and consumes a token for the given key. Returns true if allowed.
// Idle clients are evicted after 10 minutes to bound memory.
func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	// Opportunistic cleanup of stale buckets.
	if now.Sub(rl.lastSeen) > 10*time.Minute {
		for k, b := range rl.clients {
			if now.Sub(b.lastSeen) > 10*time.Minute {
				delete(rl.clients, k)
			}
		}
		rl.lastSeen = now
	}

	b, ok := rl.clients[key]
	if !ok {
		b = &clientBucket{tokens: float64(rl.burst), last: now}
		rl.clients[key] = b
	}

	// Refill tokens based on elapsed time.
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.last = now
	b.lastSeen = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// RateLimitMiddleware limits requests per tenant (or per IP in legacy mode).
// ratePerSec and burst come from env: RATE_LIMIT_RATE, RATE_LIMIT_BURST.
// When rate is 0, the middleware is disabled.
func RateLimitMiddleware(rate float64, burst int, next http.Handler) http.Handler {
	if rate <= 0 {
		return next
	}
	rl := newRateLimiter(rate, burst)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "ip:" + r.RemoteAddr
		if t, ok := TenantFromContext(r.Context()); ok {
			key = "tenant:" + t.ID
		}
		if !rl.allow(key) {
			w.Header().Set("Retry-After", strconv.Itoa(int(1/rate)+1))
			writeError(w, http.StatusTooManyRequests, ErrCodeRateLimited, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}
