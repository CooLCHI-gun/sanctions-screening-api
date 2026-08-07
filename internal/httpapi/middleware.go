package httpapi

import (
	"net/http"
	"os"
)

// APIKeyMiddleware checks for a valid API key in the X-API-Key header.
//
// If the API_KEY environment variable is not set, the middleware is
// disabled and all requests pass through — this is the default for
// local development and testing.
//
// When API_KEY is set, requests to protected endpoints must include
// a matching X-API-Key header. Missing or mismatched keys result in
// 401 Unauthorized.
//
// In a marketplace deployment (e.g. RapidAPI), this middleware would
// be replaced or augmented by the platform's own auth/rate-limiting
// layer. The X-API-Key convention is used here as a simple,
// widely-understood placeholder.
func APIKeyMiddleware(next http.Handler) http.Handler {
	expectedKey := os.Getenv("API_KEY")

	// If no API_KEY is configured, auth is disabled.
	if expectedKey == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := r.Header.Get("X-API-Key")
		if provided != expectedKey {
			writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "missing or invalid API key")
			return
		}
		next.ServeHTTP(w, r)
	})
}
