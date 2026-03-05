package provider

import (
	"context"
	"time"
)

// SourceTag is a custom iCal property used to identify blocker events
// created by wc-cal-sync and link them back to their source event.
const SourceTagProperty = "X-WC-SYNC-SOURCE"

// Event represents a calendar event normalized across all providers.
type Event struct {
	ID          string
	CalendarID  string
	ProviderID  string // config ID of the calendar this event belongs to
	Title       string
	Start       time.Time
	End         time.Time
	Location    string
	Description string
	IsAllDay    bool
	IsBusy      bool   // true = opaque/busy, false = transparent/free
	SourceTag   string // non-empty if this is a blocker created by us
	ETag        string // provider-specific version tag for change detection
}

// IsBlocker returns true if this event was created by wc-cal-sync.
func (e *Event) IsBlocker() bool {
	return e.SourceTag != ""
}

// MakeSourceTag creates a source tag for a given event.
func MakeSourceTag(providerID, calendarID, eventID string) string {
	return providerID + ":" + calendarID + ":" + eventID
}

// ListResult contains the results of a ListEvents call.
type ListResult struct {
	Events        []Event
	NextSyncToken string // for incremental sync on next call
}

// Provider is the interface that all calendar backends implement.
type Provider interface {
	// Name returns the provider type ("google", "outlook", "apple").
	Name() string

	// ListEvents returns events in the given time range.
	// Pass syncToken from a previous ListResult for incremental sync (empty string for full sync).
	ListEvents(ctx context.Context, calendarID string, start, end time.Time, syncToken string) (*ListResult, error)

	// CreateEvent creates a new event on the specified calendar.
	CreateEvent(ctx context.Context, calendarID string, event *Event) error

	// UpdateEvent updates an existing event.
	UpdateEvent(ctx context.Context, calendarID string, event *Event) error

	// DeleteEvent removes an event by ID.
	DeleteEvent(ctx context.Context, calendarID string, eventID string) error
}
