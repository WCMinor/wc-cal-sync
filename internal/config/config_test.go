package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/WCMinor/wc-cal-sync/internal/config"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

func TestLoad_Valid(t *testing.T) {
	path := writeConfig(t, `
sources:
  - name: Work
    url: https://example.com/work.ics
  - url: /tmp/personal.ics
output: /tmp/merged.ics
timeout: 10s
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(cfg.Sources))
	}
	if cfg.Sources[0].Name != "Work" {
		t.Errorf("expected sources[0].Name='Work', got %q", cfg.Sources[0].Name)
	}
	if cfg.Sources[1].URL != "/tmp/personal.ics" {
		t.Errorf("unexpected url: %q", cfg.Sources[1].URL)
	}
	if cfg.Output != "/tmp/merged.ics" {
		t.Errorf("unexpected output: %q", cfg.Output)
	}
	if cfg.Timeout != 10*time.Second {
		t.Errorf("unexpected timeout: %v", cfg.Timeout)
	}
}

func TestLoad_DefaultTimeout(t *testing.T) {
	path := writeConfig(t, `
sources:
  - url: https://example.com/cal.ics
output: /tmp/out.ics
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", cfg.Timeout)
	}
}

func TestLoad_MissingSources(t *testing.T) {
	path := writeConfig(t, `output: /tmp/out.ics`)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for missing sources")
	}
}

func TestLoad_MissingOutput(t *testing.T) {
	path := writeConfig(t, `sources:
  - url: https://example.com/cal.ics
`)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for missing output")
	}
}

func TestLoad_EmptySourceURL(t *testing.T) {
	path := writeConfig(t, `
sources:
  - name: Nameless
output: /tmp/out.ics
`)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for empty source url")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := writeConfig(t, `{not: valid: yaml`)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}
