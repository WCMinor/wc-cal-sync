package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Calendars      []CalendarConfig      `yaml:"calendars"`
	MergedCalendar MergedCalendarConfig  `yaml:"merged_calendar"`
	Sync           SyncConfig            `yaml:"sync"`
	OAuth          OAuthConfig           `yaml:"oauth"`
	StateFile      string                `yaml:"state_file"`
}

type CalendarConfig struct {
	ID             string `yaml:"id"`
	Provider       string `yaml:"provider"` // "google", "outlook", "apple"
	CalendarID     string `yaml:"calendar_id"`

	// Google OAuth
	GoogleClientID     string `yaml:"google_client_id,omitempty"`
	GoogleClientSecret string `yaml:"google_client_secret,omitempty"`

	// Outlook OAuth
	TenantID     string `yaml:"tenant_id,omitempty"`
	ClientID     string `yaml:"client_id,omitempty"`
	ClientSecret string `yaml:"client_secret,omitempty"`

	// Apple CalDAV (no OAuth — uses app-specific password)
	Server       string `yaml:"server,omitempty"`
	Username     string `yaml:"username,omitempty"`
	PasswordEnv  string `yaml:"password_env,omitempty"`
	CalendarPath string `yaml:"calendar_path,omitempty"`

	// Token file for persisted OAuth tokens
	TokenFile string `yaml:"token_file,omitempty"`
}

type MergedCalendarConfig struct {
	Provider     string `yaml:"provider"`
	Server       string `yaml:"server,omitempty"`
	Username     string `yaml:"username,omitempty"`
	PasswordEnv  string `yaml:"password_env,omitempty"`
	CalendarPath string `yaml:"calendar_path,omitempty"`
	CalendarID   string `yaml:"calendar_id,omitempty"`

	// OAuth fields for Google/Outlook merged calendar
	GoogleClientID     string `yaml:"google_client_id,omitempty"`
	GoogleClientSecret string `yaml:"google_client_secret,omitempty"`
	TenantID           string `yaml:"tenant_id,omitempty"`
	ClientID           string `yaml:"client_id,omitempty"`
	ClientSecret       string `yaml:"client_secret,omitempty"`
	TokenFile          string `yaml:"token_file,omitempty"`
}

type SyncConfig struct {
	IntervalMinutes    int      `yaml:"interval_minutes"`
	LookaheadDays      int      `yaml:"lookahead_days"`
	LookbackDays       int      `yaml:"lookback_days"`
	BlockerTitle       string   `yaml:"blocker_title"`
	LegacyBlockerWords []string `yaml:"legacy_blocker_words"` // titles to treat as legacy blockers (e.g. from Reclaim.ai)
	DryRun             bool     `yaml:"dry_run"`
}

type OAuthConfig struct {
	CallbackPort int    `yaml:"callback_port"`
	CallbackHost string `yaml:"callback_host"`
}

func (c *SyncConfig) LookaheadDuration() time.Duration {
	return time.Duration(c.LookaheadDays) * 24 * time.Hour
}

func (c *SyncConfig) LookbackDuration() time.Duration {
	return time.Duration(c.LookbackDays) * 24 * time.Hour
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	// Expand environment variables in the YAML
	expanded := os.ExpandEnv(string(data))

	cfg := &Config{
		Sync: SyncConfig{
			IntervalMinutes: 5,
			LookaheadDays:   30,
			LookbackDays:    7,
			BlockerTitle:    "Busy",
		},
		OAuth: OAuthConfig{
			CallbackPort: 8089,
			CallbackHost: "localhost",
		},
		StateFile: "sync-state.json",
	}

	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	if len(c.Calendars) == 0 {
		return fmt.Errorf("no calendars configured")
	}
	for i, cal := range c.Calendars {
		if cal.ID == "" {
			return fmt.Errorf("calendar %d: id is required", i)
		}
		switch cal.Provider {
		case "google", "outlook", "apple":
		default:
			return fmt.Errorf("calendar %s: unsupported provider %q", cal.ID, cal.Provider)
		}
	}
	return nil
}
