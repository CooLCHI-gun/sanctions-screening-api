package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/CooLCHI-gun/sanctions-screening-api/internal/audit"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/review"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/tenant"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/watchlist"
)

// Stores bundles the optional persistence stores for tenant-aware features.
// Each store may be nil when the feature is disabled (legacy mode).
type Stores struct {
	Tenant    tenant.Store
	Audit     audit.Store
	Watchlist watchlist.Store
	Review    review.Store
	Dir       string
}

// StorePaths returns default DB paths relative to baseDir.
func StorePaths(baseDir string) (tenantPath, auditPath, watchlistPath, reviewPath string) {
	return filepath.Join(baseDir, "tenants.db"),
		filepath.Join(baseDir, "audit.db"),
		filepath.Join(baseDir, "watchlists.db"),
		filepath.Join(baseDir, "review.db")
}

// InitStores opens the four SQLite stores if TENANT_MODE is enabled.
//
// TENANT_MODE=1 enables tenant-aware features (multi-tenant keys, audit,
// watchlists, review queue). When disabled, all stores are nil and the API
// runs in legacy single-key mode.
func InitStores(ctx context.Context, logger *slog.Logger) (*Stores, error) {
	if os.Getenv("TENANT_MODE") == "" || os.Getenv("TENANT_MODE") == "0" {
		logger.Info("tenant mode disabled — running in legacy single-key mode")
		return &Stores{}, nil
	}

	baseDir := os.Getenv("DATA_DIR")
	if baseDir == "" {
		baseDir = "data"
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("bootstrap: mkdir data dir: %w", err)
	}

	tenantPath, auditPath, watchlistPath, reviewPath := StorePaths(baseDir)

	ts, err := tenant.NewSQLiteStore(ctx, tenantPath)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: tenant store: %w", err)
	}
	as, err := audit.NewSQLiteStore(ctx, auditPath)
	if err != nil {
		ts.Close()
		return nil, fmt.Errorf("bootstrap: audit store: %w", err)
	}
	ws, err := watchlist.NewSQLiteStore(ctx, watchlistPath)
	if err != nil {
		ts.Close()
		as.Close()
		return nil, fmt.Errorf("bootstrap: watchlist store: %w", err)
	}
	rs, err := review.NewSQLiteStore(ctx, reviewPath)
	if err != nil {
		ts.Close()
		as.Close()
		ws.Close()
		return nil, fmt.Errorf("bootstrap: review store: %w", err)
	}

	logger.Info("tenant mode enabled — stores initialised",
		"tenant", tenantPath, "audit", auditPath,
		"watchlist", watchlistPath, "review", reviewPath)

	return &Stores{
		Tenant:    ts,
		Audit:     as,
		Watchlist: ws,
		Review:    rs,
		Dir:       baseDir,
	}, nil
}
