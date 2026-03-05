// wc-cal-sync is a command-line tool for merging multiple iCalendar sources
// (HTTP URLs or local files) into a single .ics output file.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	ical "github.com/arran4/golang-ical"
	"github.com/spf13/cobra"

	"github.com/WCMinor/wc-cal-sync/internal/calendar"
	"github.com/WCMinor/wc-cal-sync/internal/config"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "wc-cal-sync",
		Short: "A tool to sync and merge iCalendar sources into a single .ics file",
		Long: `wc-cal-sync fetches one or more iCalendar feeds (HTTP/HTTPS URLs or local
.ics files), merges their events—deduplicating by UID—and writes the result
to an output .ics file.`,
	}
	root.AddCommand(newSyncCmd())
	return root
}

func newSyncCmd() *cobra.Command {
	var (
		cfgFile string
		sources []string
		output  string
		timeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Fetch, merge, and write calendar sources",
		Long: `Fetch all configured calendar sources, merge their events (deduplicating
by UID, first occurrence wins), and write the result to the output file.

Configuration can be supplied via a YAML config file (--config) or directly
via --source and --output flags.

Example:
  wc-cal-sync sync --config config.yaml
  wc-cal-sync sync --source https://example.com/work.ics \
                   --source /home/user/personal.ics \
                   --output merged.ics`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cfgFile, sources, output, timeout)
		},
	}

	cmd.Flags().StringVarP(&cfgFile, "config", "c", "", "path to YAML config file")
	cmd.Flags().StringArrayVarP(&sources, "source", "s", nil, "calendar source URL or file path (repeatable)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output .ics file path")
	cmd.Flags().DurationVarP(&timeout, "timeout", "t", 30*time.Second, "HTTP request timeout")

	return cmd
}

func runSync(cfgFile string, sources []string, output string, timeout time.Duration) error {
	var cfg *config.Config

	switch {
	case cfgFile != "":
		var err error
		cfg, err = config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

	case len(sources) > 0 && output != "":
		cfg = &config.Config{Output: output, Timeout: timeout}
		for _, s := range sources {
			cfg.Sources = append(cfg.Sources, config.Source{URL: s})
		}

	default:
		return fmt.Errorf("provide either --config or both --source and --output flags")
	}

	fetcher := calendar.NewFetcher(cfg.Timeout)
	ctx := context.Background()

	fmt.Fprintf(os.Stdout, "Fetching %d source(s)...\n", len(cfg.Sources))

	cals := make([]*ical.Calendar, 0, len(cfg.Sources))
	for _, src := range cfg.Sources {
		label := src.URL
		if src.Name != "" {
			label = src.Name
		}
		fmt.Fprintf(os.Stdout, "  → %s\n", label)
		cal, err := fetcher.Fetch(ctx, src.URL)
		if err != nil {
			return fmt.Errorf("fetching %q: %w", src.URL, err)
		}
		cals = append(cals, cal)
	}

	fmt.Fprintln(os.Stdout, "Merging calendars...")
	merged := calendar.Merge(cals)

	fmt.Fprintf(os.Stdout, "Writing output to %s...\n", cfg.Output)
	if err := calendar.WriteFile(cfg.Output, merged); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	fmt.Fprintln(os.Stdout, "Done.")
	return nil
}
