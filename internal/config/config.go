// Package config handles loading and validating configuration for wc-cal-sync.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Source represents a single calendar source (URL or local file path).
type Source struct {
	// Name is an optional human-readable label for this source.
	Name string `yaml:"name"`
	// URL is an HTTP/HTTPS URL or a local file path to an .ics file.
	URL string `yaml:"url"`
}

// Config is the top-level configuration for a sync run.
type Config struct {
	// Sources is the list of calendars to merge.
	Sources []Source `yaml:"sources"`
	// Output is the path of the resulting merged .ics file.
	Output string `yaml:"output"`
	// Timeout is the HTTP request timeout (default 30s).
	Timeout time.Duration `yaml:"timeout"`
}

// Load reads and validates a YAML config file from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if len(c.Sources) == 0 {
		return fmt.Errorf("at least one source calendar is required")
	}
	for i, s := range c.Sources {
		if s.URL == "" {
			return fmt.Errorf("source[%d]: url must not be empty", i)
		}
	}
	if c.Output == "" {
		return fmt.Errorf("output path must not be empty")
	}
	return nil
}
