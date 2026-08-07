package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/CooLCHI-gun/sanctions-screening-api/internal/audit"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/review"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/tenant"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/watchlist"
)

// RouterOptions wires optional stores into the router. When a store is nil the
// corresponding feature degrades gracefully (legacy single-key mode).
type RouterOptions struct {
	TenantStore    tenant.Store
	AuditStore     audit.Store
	ReviewStore    review.Store
	WatchlistStore watchlist.Store
	Logger         *slog.Logger
	// RateLimitRate and RateLimitBurst configure per-tenant (or per-IP in
	// legacy mode) rate limiting. Rate <= 0 disables the limiter.
	RateLimitRate  float64
	RateLimitBurst int
	// AdminKey protects /tenants. Empty disables admin endpoints.
	AdminKey string
	// RapidProxySecret validates X-RapidAPI-Proxy-Secret (marketplace mode).
	// Empty = disabled (local development).
	RapidProxySecret string
}

// NewRouter creates and configures the HTTP router with all routes.
//
// Routing tiers:
//   - Public: /health /ready /version /metadata
//   - Tenant-auth required (when TenantStore set): /screen /cases /watchlists /audit
//   - Admin (no auth, deployment-scoped): /tenants
//
// In legacy mode (TenantStore nil), /screen remains behind the simple
// APIKeyMiddleware for backward compatibility and /cases//watchlists/audit
// are unavailable.
func NewRouter(h *Handler, opts RouterOptions) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.Health)
	mux.HandleFunc("/ready", h.Ready)
	mux.HandleFunc("/version", h.Version)
	mux.HandleFunc("/metadata", h.Metadata)

	rl := func(next http.Handler) http.Handler {
		return RateLimitMiddleware(opts.RateLimitRate, opts.RateLimitBurst, next)
	}
	proxy := func(next http.Handler) http.Handler {
		return RapidProxyMiddleware(opts.RapidProxySecret, next)
	}

	if opts.TenantStore != nil {
		// Tenant-aware routing.
		rh := &ReviewHandler{store: opts.ReviewStore, audit: opts.AuditStore, logger: opts.Logger}
		wh := &WatchlistHandler{store: opts.WatchlistStore, logger: opts.Logger}
		ah := &AuditHandler{store: opts.AuditStore, logger: opts.Logger}
		th := &TenantHandler{store: opts.TenantStore, logger: opts.Logger}

		tenantAuth := func(next http.Handler) http.Handler {
			return TenantMiddleware(opts.TenantStore, next)
		}
		fullOnly := func(next http.Handler) http.Handler {
			return requireTier(tenant.TierFull, next)
		}
		admin := func(next http.Handler) http.Handler {
			return AdminMiddleware(opts.AdminKey, next)
		}

		mux.Handle("/screen", proxy(rl(tenantAuth(http.HandlerFunc(h.ScreenTenant)))))
		mux.Handle("/cases", proxy(rl(tenantAuth(fullOnly(http.HandlerFunc(rh.ListCases))))))
		mux.Handle("/cases/", proxy(rl(tenantAuth(fullOnly(http.HandlerFunc(rh.GetOrDecide))))))
		mux.Handle("/watchlists", proxy(rl(tenantAuth(http.HandlerFunc(wh.ListWatchlists)))))
		mux.Handle("/watchlists/", proxy(rl(tenantAuth(http.HandlerFunc(wh.UploadWatchlist)))))
		mux.Handle("/audit", proxy(rl(tenantAuth(fullOnly(http.HandlerFunc(ah.ListAudit))))))
		// Admin endpoints: protected by ADMIN_API_KEY (deployment-scoped).
		mux.Handle("/tenants", proxy(rl(admin(http.HandlerFunc(th.CreateOrList)))))
		mux.Handle("/tenants/", proxy(rl(admin(http.HandlerFunc(th.ToggleLLMCascade)))))
	} else {
		// Legacy: single API key, no tenant features.
		mux.Handle("/screen", proxy(rl(APIKeyMiddleware(http.HandlerFunc(h.Screen)))))
	}

	return mux
}
