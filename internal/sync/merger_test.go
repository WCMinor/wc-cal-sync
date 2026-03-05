package sync

import (
	"context"
	"testing"
	"time"

	"github.com/user/wc-cal-sync/internal/provider"
	"github.com/user/wc-cal-sync/internal/store"
)

func TestMergedWriter_CreatesEventsForAllRealEvents(t *testing.T) {
	mock := provider.NewMockProvider("apple")
	writer := &MergedWriter{provider: mock, calendarID: "/merged/"}

	state := store.New("")
	now := time.Now()

	events := []provider.Event{
		{
			ID: "ev1", CalendarID: "c1", ProviderID: "cal1", Title: "Meeting A",
			Start: now.Add(1 * time.Hour), End: now.Add(2 * time.Hour), IsBusy: true,
		},
		{
			ID: "ev2", CalendarID: "c2", ProviderID: "cal2", Title: "Meeting B",
			Start: now.Add(3 * time.Hour), End: now.Add(4 * time.Hour), IsBusy: true,
			Location: "Room 101",
		},
	}

	ctx := context.Background()
	if err := writer.Sync(ctx, events, state); err != nil {
		t.Fatalf("Sync() error: %v", err)
	}

	if len(mock.CreatedEvents) != 2 {
		t.Fatalf("should create 2 merged events, got %d", len(mock.CreatedEvents))
	}

	// Merged events should have provider prefix in title
	if title := mock.CreatedEvents[0].Event.Title; title != "[cal1] Meeting A" {
		t.Errorf("merged title = %q, want %q", title, "[cal1] Meeting A")
	}
	if title := mock.CreatedEvents[1].Event.Title; title != "[cal2] Meeting B" {
		t.Errorf("merged title = %q, want %q", title, "[cal2] Meeting B")
	}

	// Merged events should be transparent (not blocking)
	for _, c := range mock.CreatedEvents {
		if c.Event.IsBusy {
			t.Errorf("merged event %q should be transparent", c.Event.Title)
		}
	}

	// State should track merged event IDs
	tag1 := provider.MakeSourceTag("cal1", "c1", "ev1")
	if got := state.GetMergedEventID(tag1); got == "" {
		t.Error("merged event ID should be stored in state")
	}
}

func TestMergedWriter_UpdatesExistingMergedEvents(t *testing.T) {
	mock := provider.NewMockProvider("apple")
	writer := &MergedWriter{provider: mock, calendarID: "/merged/"}

	state := store.New("")
	now := time.Now()

	events := []provider.Event{
		{
			ID: "ev1", CalendarID: "c1", ProviderID: "cal1", Title: "Meeting A",
			Start: now.Add(1 * time.Hour), End: now.Add(2 * time.Hour), IsBusy: true,
		},
	}

	ctx := context.Background()

	// First sync
	if err := writer.Sync(ctx, events, state); err != nil {
		t.Fatalf("first Sync() error: %v", err)
	}
	if len(mock.CreatedEvents) != 1 {
		t.Fatalf("first sync should create 1, got %d", len(mock.CreatedEvents))
	}

	// Second sync — should update, not create
	mock.Reset()
	if err := writer.Sync(ctx, events, state); err != nil {
		t.Fatalf("second Sync() error: %v", err)
	}
	if len(mock.CreatedEvents) != 0 {
		t.Errorf("second sync should not create new events, got %d", len(mock.CreatedEvents))
	}
	if len(mock.UpdatedEvents) != 1 {
		t.Errorf("second sync should update 1 event, got %d", len(mock.UpdatedEvents))
	}
}

func TestMergedWriter_CleansUpRemovedEvents(t *testing.T) {
	mock := provider.NewMockProvider("apple")
	writer := &MergedWriter{provider: mock, calendarID: "/merged/"}

	state := store.New("")
	now := time.Now()

	events := []provider.Event{
		{
			ID: "ev1", CalendarID: "c1", ProviderID: "cal1", Title: "Meeting A",
			Start: now.Add(1 * time.Hour), End: now.Add(2 * time.Hour), IsBusy: true,
		},
	}

	ctx := context.Background()

	// First sync — creates merged event
	if err := writer.Sync(ctx, events, state); err != nil {
		t.Fatalf("Sync() error: %v", err)
	}

	// Second sync with empty events — should delete the merged event
	mock.Reset()
	if err := writer.Sync(ctx, []provider.Event{}, state); err != nil {
		t.Fatalf("Sync() error: %v", err)
	}

	if len(mock.DeletedEvents) != 1 {
		t.Errorf("should delete 1 orphaned merged event, got %d", len(mock.DeletedEvents))
	}

	// State should be cleaned
	tag := provider.MakeSourceTag("cal1", "c1", "ev1")
	if got := state.GetMergedEventID(tag); got != "" {
		t.Errorf("merged event should be removed from state, got %q", got)
	}
}

func TestMergedWriter_PreservesLocation(t *testing.T) {
	mock := provider.NewMockProvider("apple")
	writer := &MergedWriter{provider: mock, calendarID: "/merged/"}

	state := store.New("")
	now := time.Now()

	events := []provider.Event{
		{
			ID: "ev1", CalendarID: "c1", ProviderID: "cal1", Title: "Offsite",
			Start: now.Add(1 * time.Hour), End: now.Add(2 * time.Hour),
			Location: "Conference Room B", Description: "Weekly planning",
			IsBusy: true,
		},
	}

	ctx := context.Background()
	if err := writer.Sync(ctx, events, state); err != nil {
		t.Fatalf("Sync() error: %v", err)
	}

	created := mock.CreatedEvents[0].Event
	if created.Location != "Conference Room B" {
		t.Errorf("location = %q, want %q", created.Location, "Conference Room B")
	}
	if created.Description != "Weekly planning" {
		t.Errorf("description = %q, want %q", created.Description, "Weekly planning")
	}
}
