package sync

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/user/wc-cal-sync/internal/config"
	"github.com/user/wc-cal-sync/internal/provider"
	"github.com/user/wc-cal-sync/internal/store"
)

// Engine orchestrates the calendar sync process.
type Engine struct {
	cfg       *config.Config
	providers map[string]provider.Provider // keyed by calendar config ID
	merged    *MergedWriter
	state     *store.State
}

// CalendarEntry pairs a config with its provider.
type CalendarEntry struct {
	Config   config.CalendarConfig
	Provider provider.Provider
}

func NewEngine(cfg *config.Config, providers map[string]provider.Provider, merged *MergedWriter, state *store.State) *Engine {
	return &Engine{
		cfg:       cfg,
		providers: providers,
		merged:    merged,
		state:     state,
	}
}

// Run executes one full sync cycle.
func (e *Engine) Run(ctx context.Context) error {
	log.Println("Starting sync cycle...")

	now := time.Now()
	start := now.Add(-e.cfg.Sync.LookbackDuration())
	end := now.Add(e.cfg.Sync.LookaheadDuration())

	// Phase 1: Fetch all events from all calendars
	allEvents := make(map[string][]provider.Event) // calConfigID -> events
	for _, cal := range e.cfg.Calendars {
		prov, ok := e.providers[cal.ID]
		if !ok {
			log.Printf("No provider for calendar %s, skipping", cal.ID)
			continue
		}

		syncToken := e.state.GetSyncToken(cal.ID)
		result, err := prov.ListEvents(ctx, cal.CalendarID, start, end, syncToken)
		if err != nil {
			log.Printf("Error listing events for %s: %v", cal.ID, err)
			continue
		}

		// Tag events with their provider ID
		for i := range result.Events {
			result.Events[i].ProviderID = cal.ID
		}

		allEvents[cal.ID] = result.Events
		if result.NextSyncToken != "" {
			e.state.SetSyncToken(cal.ID, result.NextSyncToken)
		}

		log.Printf("Fetched %d events from %s", len(result.Events), cal.ID)
	}

	// Phase 2: For each real event, ensure blockers exist on all OTHER calendars
	realEvents := e.collectRealEvents(allEvents)
	log.Printf("Found %d real (non-blocker) events across all calendars", len(realEvents))

	if err := e.syncBlockers(ctx, realEvents); err != nil {
		log.Printf("Error syncing blockers: %v", err)
	}

	// Phase 3: Write real events to merged calendar
	if e.merged != nil {
		if err := e.merged.Sync(ctx, realEvents, e.state); err != nil {
			log.Printf("Error syncing merged calendar: %v", err)
		}
	}

	// Phase 4: Clean up orphaned blockers
	if err := e.cleanupOrphanedBlockers(ctx, realEvents); err != nil {
		log.Printf("Error cleaning up blockers: %v", err)
	}

	// Save state
	if err := e.state.Save(); err != nil {
		log.Printf("Error saving state: %v", err)
	}

	log.Println("Sync cycle complete.")
	return nil
}

// collectRealEvents filters out blocker events, returning only real ones.
func (e *Engine) collectRealEvents(allEvents map[string][]provider.Event) []provider.Event {
	var real []provider.Event
	for _, events := range allEvents {
		for _, ev := range events {
			if !ev.IsBlocker() && ev.Title != "" {
				real = append(real, ev)
			}
		}
	}
	return real
}

// syncBlockers ensures that for each real event, busy blockers exist on all other calendars.
func (e *Engine) syncBlockers(ctx context.Context, realEvents []provider.Event) error {
	for _, ev := range realEvents {
		if !ev.IsBusy {
			continue // don't block time for free/transparent events
		}

		sourceTag := provider.MakeSourceTag(ev.ProviderID, ev.CalendarID, ev.ID)
		existingBlockers := e.state.GetBlockers(sourceTag)

		// Build a set of calendars that already have blockers
		hasBlocker := make(map[string]bool)
		for _, b := range existingBlockers {
			hasBlocker[b.CalendarConfigID] = true
		}

		// Create blockers on all calendars that don't have one yet
		for _, cal := range e.cfg.Calendars {
			if cal.ID == ev.ProviderID {
				continue // don't create blocker on the source calendar
			}
			if hasBlocker[cal.ID] {
				continue // already has a blocker
			}

			prov, ok := e.providers[cal.ID]
			if !ok {
				continue
			}

			blocker := &provider.Event{
				CalendarID: cal.CalendarID,
				Title:      e.cfg.Sync.BlockerTitle,
				Start:      ev.Start,
				End:        ev.End,
				IsAllDay:   ev.IsAllDay,
				IsBusy:     true,
				SourceTag:  sourceTag,
			}

			if err := prov.CreateEvent(ctx, cal.CalendarID, blocker); err != nil {
				log.Printf("Failed to create blocker on %s for event %s: %v", cal.ID, ev.ID, err)
				continue
			}

			e.state.AddBlocker(sourceTag, store.BlockerRef{
				CalendarConfigID: cal.ID,
				CalendarID:       cal.CalendarID,
				EventID:          blocker.ID,
			})

			log.Printf("Created blocker on %s for %q (%s - %s)",
				cal.ID, ev.Title, ev.Start.Format(time.RFC822), ev.End.Format(time.RFC822))
		}
	}

	return nil
}

// cleanupOrphanedBlockers removes blockers whose source events no longer exist.
func (e *Engine) cleanupOrphanedBlockers(ctx context.Context, realEvents []provider.Event) error {
	// Build set of current source tags
	currentTags := make(map[string]bool)
	for _, ev := range realEvents {
		tag := provider.MakeSourceTag(ev.ProviderID, ev.CalendarID, ev.ID)
		currentTags[tag] = true
	}

	// Check all stored blocker source tags
	for _, sourceTag := range e.state.AllBlockerSourceTags() {
		if currentTags[sourceTag] {
			continue // source event still exists
		}

		// Source event is gone — delete all its blockers
		blockers := e.state.GetBlockers(sourceTag)
		for _, b := range blockers {
			prov, ok := e.providers[b.CalendarConfigID]
			if !ok {
				continue
			}
			if err := prov.DeleteEvent(ctx, b.CalendarID, b.EventID); err != nil {
				log.Printf("Failed to delete orphaned blocker %s on %s: %v", b.EventID, b.CalendarConfigID, err)
				continue
			}
			log.Printf("Deleted orphaned blocker %s on %s", b.EventID, b.CalendarConfigID)
		}
		e.state.RemoveBlockers(sourceTag)
	}

	return nil
}

// BuildProviders creates all provider instances from config.
func BuildProviders(ctx context.Context, cfg *config.Config) (map[string]provider.Provider, error) {
	providers := make(map[string]provider.Provider)
	callbackPort := cfg.OAuth.CallbackPort

	for _, cal := range cfg.Calendars {
		var prov provider.Provider
		var err error

		switch cal.Provider {
		case "google":
			prov, err = provider.NewGoogleProvider(ctx, cal, callbackPort)
		case "outlook":
			prov, err = provider.NewOutlookProvider(ctx, cal, callbackPort)
		case "apple":
			prov, err = provider.NewAppleProviderFromCalendarConfig(ctx, &appleConfigAdapter{cal})
		default:
			return nil, fmt.Errorf("unsupported provider %q for calendar %s", cal.Provider, cal.ID)
		}

		if err != nil {
			return nil, fmt.Errorf("initializing provider for %s: %w", cal.ID, err)
		}

		providers[cal.ID] = prov
	}

	return providers, nil
}

// appleConfigAdapter adapts CalendarConfig to the AppleProvider interface.
type appleConfigAdapter struct {
	cal config.CalendarConfig
}

func (a *appleConfigAdapter) GetAppleConfig() (string, string, string) {
	return a.cal.Server, a.cal.Username, a.cal.PasswordEnv
}
