package httpapi

import (
	"crypto/subtle"
	"net/http"
)

// RapidProxyMiddleware validates the X-RapidAPI-Proxy-Secret header that the
// RapidAPI runtime appends to every proxied request. When configured
// (RAPIDAPI_PROXY_SECRET env), only requests that carry the exact secret are
// served — this prevents consumers from bypassing RapidAPI billing by calling
// the origin directly.
//
// Behavior:
//   - If the secret is empty (local development), the middleware passes through.
//   - If set, requests without the header or with a mismatched value get 401.
func RapidProxyMiddleware(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if secret == "" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("X-RapidAPI-Proxy-Secret")
		if subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
			writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "invalid proxy secret")
			return
		}
		next.ServeHTTP(w, r)
	})
}
