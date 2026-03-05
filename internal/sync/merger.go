package sync

import (
	"context"
	"fmt"
	"log"

	"github.com/user/wc-cal-sync/internal/config"
	"github.com/user/wc-cal-sync/internal/provider"
	"github.com/user/wc-cal-sync/internal/store"
)

// MergedWriter writes all real events to a single merged calendar.
type MergedWriter struct {
	provider   provider.Provider
	calendarID string
}

func NewMergedWriter(ctx context.Context, cfg *config.Config) (*MergedWriter, error) {
	mc := cfg.MergedCalendar
	callbackPort := cfg.OAuth.CallbackPort

	var prov provider.Provider
	var err error
	var calID string

	switch mc.Provider {
	case "google":
		prov, err = provider.NewGoogleProviderFromMerged(ctx, mc, callbackPort)
		calID = mc.CalendarID
	case "outlook":
		prov, err = provider.NewOutlookProviderFromMerged(ctx, mc, callbackPort)
		calID = mc.CalendarID
	case "apple":
		adapter := &mergedAppleAdapter{mc}
		prov, err = provider.NewAppleProvider(ctx, adapter)
		calID = mc.CalendarPath
	default:
		return nil, fmt.Errorf("unsupported merged calendar provider: %s", mc.Provider)
	}

	if err != nil {
		return nil, fmt.Errorf("initializing merged calendar provider: %w", err)
	}

	return &MergedWriter{provider: prov, calendarID: calID}, nil
}

type mergedAppleAdapter struct {
	cfg config.MergedCalendarConfig
}

func (m *mergedAppleAdapter) GetAppleConfig() (string, string, string) {
	return m.cfg.Server, m.cfg.Username, m.cfg.PasswordEnv
}

// Sync writes real events to the merged calendar and cleans up stale ones.
func (m *MergedWriter) Sync(ctx context.Context, realEvents []provider.Event, state *store.State) error {
	log.Printf("Syncing %d events to merged calendar", len(realEvents))

	// Build set of current source tags
	currentTags := make(map[string]bool)

	for _, ev := range realEvents {
		sourceTag := provider.MakeSourceTag(ev.ProviderID, ev.CalendarID, ev.ID)
		currentTags[sourceTag] = true

		existingID := state.GetMergedEventID(sourceTag)
		if existingID != "" {
			// Update existing merged event
			mergedEvent := m.toMergedEvent(&ev, sourceTag)
			mergedEvent.ID = existingID
			if err := m.provider.UpdateEvent(ctx, m.calendarID, mergedEvent); err != nil {
				log.Printf("Failed to update merged event for %s: %v", sourceTag, err)
				// If update fails, try to recreate
				if err := m.provider.DeleteEvent(ctx, m.calendarID, existingID); err != nil {
					log.Printf("Failed to delete stale merged event %s: %v", existingID, err)
				}
				state.RemoveMergedEvent(sourceTag)
				existingID = ""
			}
		}

		if existingID == "" {
			// Create new merged event
			mergedEvent := m.toMergedEvent(&ev, sourceTag)
			if err := m.provider.CreateEvent(ctx, m.calendarID, mergedEvent); err != nil {
				log.Printf("Failed to create merged event for %s: %v", sourceTag, err)
				continue
			}
			state.SetMergedEventID(sourceTag, mergedEvent.ID)
			log.Printf("Created merged event: %q", ev.Title)
		}
	}

	// Clean up merged events whose source no longer exists
	for _, sourceTag := range state.AllMergedSourceTags() {
		if currentTags[sourceTag] {
			continue
		}
		eventID := state.GetMergedEventID(sourceTag)
		if err := m.provider.DeleteEvent(ctx, m.calendarID, eventID); err != nil {
			log.Printf("Failed to delete orphaned merged event %s: %v", eventID, err)
			continue
		}
		state.RemoveMergedEvent(sourceTag)
		log.Printf("Deleted orphaned merged event %s", eventID)
	}

	return nil
}

// toMergedEvent creates a full-detail event for the merged calendar.
func (m *MergedWriter) toMergedEvent(ev *provider.Event, sourceTag string) *provider.Event {
	return &provider.Event{
		CalendarID:  m.calendarID,
		Title:       fmt.Sprintf("[%s] %s", ev.ProviderID, ev.Title),
		Start:       ev.Start,
		End:         ev.End,
		Location:    ev.Location,
		Description: ev.Description,
		IsAllDay:    ev.IsAllDay,
		IsBusy:      false, // merged calendar events are transparent (informational)
		SourceTag:   sourceTag,
	}
}
