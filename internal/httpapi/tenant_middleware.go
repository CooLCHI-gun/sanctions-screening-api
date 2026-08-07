package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/CooLCHI-gun/sanctions-screening-api/internal/tenant"
)

// ctxKey is a private type for request context keys.
type ctxKey int

const (
	ctxTenantKey ctxKey = iota
)

// TenantContext carries the authenticated tenant through a request.
type TenantContext struct {
	Tenant *tenant.Tenant
}

// TenantFromContext returns the tenant attached by TenantMiddleware.
func TenantFromContext(ctx context.Context) (*tenant.Tenant, bool) {
	t, ok := ctx.Value(ctxTenantKey).(*tenant.Tenant)
	return t, ok
}

// TenantMiddleware validates the X-API-Key header against the tenant store and
// injects the tenant into the request context.
//
// Behavior:
//   - If no tenant store is configured (nil), the middleware passes through
//     (legacy single-key mode / local development).
//   - If the store is configured but no key is provided → 401.
//   - Unknown key → 401.
//   - Known key → attach tenant, call next.
func TenantMiddleware(st tenant.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if st == nil {
			next.ServeHTTP(w, r)
			return
		}
		key := r.Header.Get("X-API-Key")
		if key == "" {
			writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "missing API key")
			return
		}
		t, err := st.LookupByAPIKey(r.Context(), key)
		if err != nil {
			if errors.Is(err, tenant.ErrNotFound) {
				writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "invalid API key")
				return
			}
			writeError(w, http.StatusInternalServerError, ErrCodeInternal, "tenant lookup failed")
			return
		}
		ctx := context.WithValue(r.Context(), ctxTenantKey, t)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireTier returns a middleware that enforces a minimum tenant tier.
// When no tenant is present (legacy mode), full access is allowed.
func requireTier(min tenant.Tier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t, ok := TenantFromContext(r.Context())
		if !ok {
			// Legacy / unauthenticated mode: allow (matches existing behavior).
			next.ServeHTTP(w, r)
			return
		}
		if t.Tier != tenant.TierFull && t.Tier != min {
			writeError(w, http.StatusForbidden, ErrCodeForbidden, "insufficient permission tier")
			return
		}
		next.ServeHTTP(w, r)
	})
}
