package httpapi

import (
	"crypto/subtle"
	"net/http"
)

// AdminMiddleware protects admin endpoints (e.g. /tenants) with a shared
// admin key from the ADMIN_API_KEY env var.
//
// Behavior:
//   - If ADMIN_API_KEY is empty, admin endpoints are disabled (503) —
//     safer default than open admin access.
//   - If set, requests must carry X-Admin-Key matching the env value
//     (constant-time comparison).
func AdminMiddleware(adminKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if adminKey == "" {
			writeError(w, http.StatusServiceUnavailable, ErrCodeForbidden, "admin endpoints disabled: ADMIN_API_KEY not configured")
			return
		}
		got := r.Header.Get("X-Admin-Key")
		if subtle.ConstantTimeCompare([]byte(got), []byte(adminKey)) != 1 {
			writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "invalid admin key")
			return
		}
		next.ServeHTTP(w, r)
	})
}
