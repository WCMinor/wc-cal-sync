package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
calendars:
  - id: "test-google"
    provider: "google"
    calendar_id: "primary"
    google_client_id: "client-id"
    google_client_secret: "client-secret"

  - id: "test-outlook"
    provider: "outlook"
    calendar_id: "cal-123"
    tenant_id: "common"
    client_id: "outlook-id"
    client_secret: "outlook-secret"

sync:
  interval_minutes: 10
  lookahead_days: 14
  lookback_days: 3
  blocker_title: "Hold"
  legacy_blocker_words:
    - "Busy"
    - "Busy (via Reclaim)"

state_file: "/tmp/test-state.json"
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(cfg.Calendars) != 2 {
		t.Errorf("Calendars len = %d, want 2", len(cfg.Calendars))
	}

	if cfg.Calendars[0].ID != "test-google" {
		t.Errorf("cal[0].ID = %q, want %q", cfg.Calendars[0].ID, "test-google")
	}

	if cfg.Sync.IntervalMinutes != 10 {
		t.Errorf("IntervalMinutes = %d, want 10", cfg.Sync.IntervalMinutes)
	}

	if cfg.Sync.BlockerTitle != "Hold" {
		t.Errorf("BlockerTitle = %q, want %q", cfg.Sync.BlockerTitle, "Hold")
	}

	if len(cfg.Sync.LegacyBlockerWords) != 2 {
		t.Errorf("LegacyBlockerWords len = %d, want 2", len(cfg.Sync.LegacyBlockerWords))
	}

	if cfg.StateFile != "/tmp/test-state.json" {
		t.Errorf("StateFile = %q, want %q", cfg.StateFile, "/tmp/test-state.json")
	}
}

func TestLoad_Defaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
calendars:
  - id: "test"
    provider: "google"
    calendar_id: "primary"
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Sync.IntervalMinutes != 5 {
		t.Errorf("default IntervalMinutes = %d, want 5", cfg.Sync.IntervalMinutes)
	}
	if cfg.Sync.LookaheadDays != 30 {
		t.Errorf("default LookaheadDays = %d, want 30", cfg.Sync.LookaheadDays)
	}
	if cfg.Sync.LookbackDays != 7 {
		t.Errorf("default LookbackDays = %d, want 7", cfg.Sync.LookbackDays)
	}
	if cfg.Sync.BlockerTitle != "Busy" {
		t.Errorf("default BlockerTitle = %q, want %q", cfg.Sync.BlockerTitle, "Busy")
	}
	if cfg.OAuth.CallbackPort != 8089 {
		t.Errorf("default CallbackPort = %d, want 8089", cfg.OAuth.CallbackPort)
	}
}

func TestLoad_EnvExpansion(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	os.Setenv("TEST_WC_CLIENT_ID", "expanded-id")
	defer os.Unsetenv("TEST_WC_CLIENT_ID")

	yaml := `
calendars:
  - id: "test"
    provider: "google"
    calendar_id: "primary"
    google_client_id: "${TEST_WC_CLIENT_ID}"
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Calendars[0].GoogleClientID != "expanded-id" {
		t.Errorf("GoogleClientID = %q, want %q", cfg.Calendars[0].GoogleClientID, "expanded-id")
	}
}

func TestLoad_InvalidProvider(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
calendars:
  - id: "test"
    provider: "notion"
    calendar_id: "primary"
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("Load() should fail for unsupported provider")
	}
}

func TestLoad_NoCalendars(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `calendars: []`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("Load() should fail with no calendars")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("Load() should fail for missing file")
	}
}

func TestSyncConfig_Durations(t *testing.T) {
	sc := SyncConfig{LookaheadDays: 14, LookbackDays: 3}

	if got := sc.LookaheadDuration(); got != 14*24*time.Hour {
		t.Errorf("LookaheadDuration = %v, want %v", got, 14*24*time.Hour)
	}
	if got := sc.LookbackDuration(); got != 3*24*time.Hour {
		t.Errorf("LookbackDuration = %v, want %v", got, 3*24*time.Hour)
	}
}
