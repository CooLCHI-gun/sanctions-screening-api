package bootstrap

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
)

const (
	localDataPath = "../../data/raw/sdn-sample.json"
	ofacDataPath  = "../../data/raw/ofac-sdn-sample.xml"
)

func TestLoadSanctionsRecords_Local(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	records, err := LoadSanctionsRecords(context.Background(), logger, "local", localDataPath)
	if err != nil {
		t.Fatalf("LoadSanctionsRecords failed: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
}

func TestLoadSanctionsRecords_OFAC(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	records, err := LoadSanctionsRecords(context.Background(), logger, "ofac", ofacDataPath)
	if err != nil {
		t.Fatalf("LoadSanctionsRecords (ofac) failed: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records from ofac fixture, got %d", len(records))
	}
}

func TestLoadSanctionsRecords_NonexistentPath(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	_, err := LoadSanctionsRecords(context.Background(), logger, "local", "nonexistent.json")
	if err == nil {
		t.Fatal("expected error for nonexistent path, got nil")
	}
}

func TestLoadSanctionsRecords_RecordContent(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	records, err := LoadSanctionsRecords(context.Background(), logger, "local", localDataPath)
	if err != nil {
		t.Fatalf("LoadSanctionsRecords failed: %v", err)
	}

	if records[0].Names[0].FullName != "JUAN MANUEL SANTOS" {
		t.Errorf("expected first record name JUAN MANUEL SANTOS, got %q", records[0].Names[0].FullName)
	}
	if len(records[0].Aliases) != 2 {
		t.Errorf("expected 2 aliases for first record, got %d", len(records[0].Aliases))
	}
	if records[1].EntityType != "organization" {
		t.Errorf("expected second record entity type 'organization', got %q", records[1].EntityType)
	}
	if len(records[2].Aliases) != 0 {
		t.Errorf("expected 0 aliases for third record, got %d", len(records[2].Aliases))
	}
}

func TestLoadSanctionsRecords_NormalizedPath(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Use the normalized test fixture (real data file is 19k+ records)
	normalizedPath := "../../data/normalized/sample-fixture.json"
	t.Setenv("NORMALIZED_DATA_PATH", normalizedPath)

	records, err := LoadSanctionsRecords(context.Background(), logger, "ofac", "../../data/raw/ofac-sdn-sample.xml")
	if err != nil {
		t.Fatalf("LoadSanctionsRecords via normalized path failed: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records from normalized fixture, got %d", len(records))
	}
	if records[0].Names[0].FullName != "JUAN SANTOS" {
		t.Errorf("expected first record 'JUAN SANTOS', got %q", records[0].Names[0].FullName)
	}
	// Date should be normalized to ISO format
	if records[0].Dates[0].Value != "1951-08-10" {
		t.Errorf("expected normalized date '1951-08-10', got %q", records[0].Dates[0].Value)
	}
}

func TestLoadSanctionsRecords_NormalizedPathDisabled(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Setting NORMALIZED_DATA_PATH=none skips the fast path, forcing provider load
	t.Setenv("NORMALIZED_DATA_PATH", "none")

	records, err := LoadSanctionsRecords(context.Background(), logger, "ofac", ofacDataPath)
	if err != nil {
		t.Fatalf("LoadSanctionsRecords with normalized path disabled failed: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records from ofac fixture, got %d", len(records))
	}
}

func TestLoadSanctionsRecords_LocalProviderNoNormalized(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Local provider doesn't have a default normalized path — should load via provider.
	records, err := LoadSanctionsRecords(context.Background(), logger, "local", localDataPath)
	if err != nil {
		t.Fatalf("LoadSanctionsRecords failed: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records from local provider, got %d", len(records))
	}
}

func TestMain(m *testing.M) {
	// Verify data files exist before running tests
	for _, path := range []string{localDataPath, ofacDataPath} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			altPath := "data/raw/" + path[strings.LastIndex(path, "/")+1:]
			if _, err := os.Stat(altPath); os.IsNotExist(err) {
				panic("test data file not found at " + path + " or " + altPath)
			}
		}
	}
	os.Exit(m.Run())
}
