package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/CooLCHI-gun/sanctions-screening-api/internal/models"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/providers"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/providers/euconsolidated"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/providers/localsource"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/providers/ofac"
	"github.com/CooLCHI-gun/sanctions-screening-api/internal/providers/unsc"
)

// normalizedPathForProvider returns the default pre-normalised data path for
// the given provider type. Returns empty string for providers that load
// directly from raw (no normalisation step).
//
// Override via NORMALIZED_DATA_PATH env var. Set to "none" to force
// provider-based loading from raw data.
func normalizedPathForProvider(providerType string) string {
	if v := os.Getenv("NORMALIZED_DATA_PATH"); v != "" {
		if v == "none" {
			return ""
		}
		return v
	}
	switch providerType {
	case "ofac":
		return "data/normalized/ofac-records.json"
	default:
		// Local provider loads from raw JSON directly — no normalisation step.
		return ""
	}
}

// LoadSanctionsRecords reads sanctions data with the following strategy:
//
//  1. If a pre-normalised JSON file exists (provider-aware default path,
//     or NORMALIZED_DATA_PATH override), it is loaded directly — fast path.
//
//  2. Otherwise, the raw data is loaded and normalised via the specified
//     provider type and source path.
//
// Supported providerType values: "local" (default) or "ofac".
func LoadSanctionsRecords(ctx context.Context, logger *slog.Logger, providerType, sourcePath string) ([]models.SanctionsRecord, error) {
	// 1. Try normalised fast path (provider-aware)
	normalizedPath := normalizedPathForProvider(providerType)
	if normalizedPath != "" {
		if records, nerr := tryNormalizedPath(logger, normalizedPath); nerr == nil {
			return records, nil
		} else {
			logger.Debug("normalised data not available, loading via provider",
				"path", normalizedPath,
				"reason", nerr,
			)
		}
	}

	// 2. Fall back to provider-based loading from raw
	var p providers.Provider
	switch providerType {
	case "ofac":
		p = ofac.NewProvider(sourcePath)
	case "eu", "euconsolidated":
		p = euconsolidated.NewProvider(sourcePath)
	case "un", "unsc", "un-security-council":
		p = unsc.NewProvider(sourcePath)
	default:
		p = localsource.NewProvider(sourcePath)
	}

	records, err := p.Fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("bootstrap (%s): %w", providerType, err)
	}

	logger.Info("sanctions data loaded via provider",
		"provider", p.Name(),
		"source", sourcePath,
		"records", len(records),
	)
	return records, nil
}

// tryNormalizedPath attempts to load records from the given pre-normalised file.
// Returns (records, nil) on success or (nil, error) if the file is missing,
// unreadable, or contains invalid data.
func tryNormalizedPath(logger *slog.Logger, path string) ([]models.SanctionsRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var records []models.SanctionsRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("%s contains 0 records", path)
	}

	logger.Info("sanctions data loaded from normalised file",
		"source", path,
		"records", len(records),
	)
	return records, nil
}
