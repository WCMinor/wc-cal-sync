package sync

import (
	"context"
	"testing"
	"time"

	"github.com/user/wc-cal-sync/internal/config"
	"github.com/user/wc-cal-sync/internal/provider"
	"github.com/user/wc-cal-sync/internal/store"
)

// helper to create a test engine with mock providers
func newTestEngine(t *testing.T, calConfigs []config.CalendarConfig, syncCfg config.SyncConfig) (*Engine, map[string]*provider.MockProvider, *store.State) {
	t.Helper()

	if syncCfg.BlockerTitle == "" {
		syncCfg.BlockerTitle = "Busy"
	}
	if syncCfg.LookaheadDays == 0 {
		syncCfg.LookaheadDays = 30
	}
	if syncCfg.LookbackDays == 0 {
		syncCfg.LookbackDays = 7
	}

	cfg := &config.Config{
		Calendars: calConfigs,
		Sync:      syncCfg,
		StateFile: "", // in-memory
	}

	mocks := make(map[string]*provider.MockProvider)
	providers := make(map[string]provider.Provider)
	for _, cal := range calConfigs {
		m := provider.NewMockProvider(cal.Provider)
		mocks[cal.ID] = m
		providers[cal.ID] = m
	}

	state := store.New("")

	engine := NewEngine(cfg, providers, nil, state)
	return engine, mocks, state
}

func TestEngine_BasicBlockerCreation(t *testing.T) {
	cals := []config.CalendarConfig{
		{ID: "google1", Provider: "google", CalendarID: "primary"},
		{ID: "outlook1", Provider: "outlook", CalendarID: "cal-ol"},
	}

	engine, mocks, _ := newTestEngine(t, cals, config.SyncConfig{})

	// Add a real event on google1
	now := time.Now()
	mocks["google1"].AddEvents("primary", provider.Event{
		ID:         "ev1",
		CalendarID: "primary",
		Title:      "Team Standup",
		Start:      now.Add(1 * time.Hour),
		End:        now.Add(2 * time.Hour),
		IsBusy:     true,
	})

	ctx := context.Background()
	if err := engine.Run(ctx); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Should have created one blocker on outlook1
	olMock := mocks["outlook1"]
	if len(olMock.CreatedEvents) != 1 {
		t.Fatalf("outlook1 CreatedEvents = %d, want 1", len(olMock.CreatedEvents))
	}

	blocker := olMock.CreatedEvents[0]
	if blocker.Event.Title != "Busy" {
		t.Errorf("blocker title = %q, want %q", blocker.Event.Title, "Busy")
	}
	if blocker.Event.SourceTag == "" {
		t.Error("blocker should have a SourceTag")
	}
	if !blocker.Event.IsBusy {
		t.Error("blocker should be busy")
	}

	// Google1 should have NO events created on it (it's the source)
	if len(mocks["google1"].CreatedEvents) != 0 {
		t.Errorf("google1 CreatedEvents = %d, want 0", len(mocks["google1"].CreatedEvents))
	}
}

func TestEngine_BlockersOnAllOtherCalendars(t *testing.T) {
	cals := []config.CalendarConfig{
		{ID: "cal1", Provider: "google", CalendarID: "c1"},
		{ID: "cal2", Provider: "outlook", CalendarID: "c2"},
		{ID: "cal3", Provider: "apple", CalendarID: "c3"},
	}

	engine, mocks, _ := newTestEngine(t, cals, config.SyncConfig{})

	now := time.Now()
	mocks["cal1"].AddEvents("c1", provider.Event{
		ID: "ev1", CalendarID: "c1", Title: "Meeting",
		Start: now.Add(1 * time.Hour), End: now.Add(2 * time.Hour), IsBusy: true,
	})

	ctx := context.Background()
	if err := engine.Run(ctx); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Blockers should be created on cal2 and cal3, not on cal1
	if len(mocks["cal1"].CreatedEvents) != 0 {
		t.Errorf("cal1 (source) should have 0 created, got %d", len(mocks["cal1"].CreatedEvents))
	}
	if len(mocks["cal2"].CreatedEvents) != 1 {
		t.Errorf("cal2 should have 1 blocker, got %d", len(mocks["cal2"].CreatedEvents))
	}
	if len(mocks["cal3"].CreatedEvents) != 1 {
		t.Errorf("cal3 should have 1 blocker, got %d", len(mocks["cal3"].CreatedEvents))
	}
}

func TestEngine_NoDuplicateBlockers(t *testing.T) {
	cals := []config.CalendarConfig{
		{ID: "cal1", Provider: "google", CalendarID: "c1"},
		{ID: "cal2", Provider: "outlook", CalendarID: "c2"},
	}

	engine, mocks, _ := newTestEngine(t, cals, config.SyncConfig{})

	now := time.Now()
	mocks["cal1"].AddEvents("c1", provider.Event{
		ID: "ev1", CalendarID: "c1", Title: "Meeting",
		Start: now.Add(1 * time.Hour), End: now.Add(2 * time.Hour), IsBusy: true,
	})

	ctx := context.Background()

	// First run
	if err := engine.Run(ctx); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(mocks["cal2"].CreatedEvents) != 1 {
		t.Fatalf("first run: cal2 should have 1 blocker, got %d", len(mocks["cal2"].CreatedEvents))
	}

	// Second run — should NOT create another blocker
	mocks["cal2"].Reset()
	if err := engine.Run(ctx); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(mocks["cal2"].CreatedEvents) != 0 {
		t.Errorf("second run: cal2 should have 0 new blockers, got %d", len(mocks["cal2"].CreatedEvents))
	}
}

func TestEngine_FreeEventsNotBlocked(t *testing.T) {
	cals := []config.CalendarConfig{
		{ID: "cal1", Provider: "google", CalendarID: "c1"},
		{ID: "cal2", Provider: "outlook", CalendarID: "c2"},
	}

	engine, mocks, _ := newTestEngine(t, cals, config.SyncConfig{})

	now := time.Now()
	mocks["cal1"].AddEvents("c1", provider.Event{
		ID: "ev1", CalendarID: "c1", Title: "Optional Lunch",
		Start: now.Add(1 * time.Hour), End: now.Add(2 * time.Hour), IsBusy: false, // transparent
	})

	ctx := context.Background()
	if err := engine.Run(ctx); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if len(mocks["cal2"].CreatedEvents) != 0 {
		t.Errorf("free events should not create blockers, got %d", len(mocks["cal2"].CreatedEvents))
	}
}

func TestEngine_LegacyBlockerSkipped(t *testing.T) {
	cals := []config.CalendarConfig{
		{ID: "cal1", Provider: "google", CalendarID: "c1"},
		{ID: "cal2", Provider: "outlook", CalendarID: "c2"},
	}

	syncCfg := config.SyncConfig{
		LegacyBlockerWords: []string{"Busy", "Busy (via Reclaim)", "[Reclaim]"},
	}

	engine, mocks, _ := newTestEngine(t, cals, syncCfg)

	now := time.Now()
	mocks["cal1"].AddEvents("c1",
		// Legacy "Busy" from Reclaim.ai — should be skipped
		provider.Event{
			ID: "reclaim1", CalendarID: "c1", Title: "Busy",
			Start: now.Add(1 * time.Hour), End: now.Add(2 * time.Hour), IsBusy: true,
		},
		// Legacy with Reclaim prefix
		provider.Event{
			ID: "reclaim2", CalendarID: "c1", Title: "Busy (via Reclaim)",
			Start: now.Add(3 * time.Hour), End: now.Add(4 * time.Hour), IsBusy: true,
		},
		// Reclaim tag
		provider.Event{
			ID: "reclaim3", CalendarID: "c1", Title: "[Reclaim] Focus Time",
			Start: now.Add(5 * time.Hour), End: now.Add(6 * time.Hour), IsBusy: true,
		},
		// Real event — should be synced
		provider.Event{
			ID: "real1", CalendarID: "c1", Title: "Actual Meeting",
			Start: now.Add(7 * time.Hour), End: now.Add(8 * time.Hour), IsBusy: true,
		},
	)

	ctx := context.Background()
	if err := engine.Run(ctx); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Only the real event should create a blocker
	if len(mocks["cal2"].CreatedEvents) != 1 {
		t.Fatalf("should create 1 blocker for real event, got %d", len(mocks["cal2"].CreatedEvents))
	}
	if mocks["cal2"].CreatedEvents[0].Event.Title != "Busy" {
		t.Errorf("blocker title = %q, want %q", mocks["cal2"].CreatedEvents[0].Event.Title, "Busy")
	}
}

func TestEngine_LegacyBlockerExactMatchBlockerTitle(t *testing.T) {
	// Even without legacy_blocker_words, events matching blocker_title should be skipped
	cals := []config.CalendarConfig{
		{ID: "cal1", Provider: "google", CalendarID: "c1"},
		{ID: "cal2", Provider: "outlook", CalendarID: "c2"},
	}

	engine, mocks, _ := newTestEngine(t, cals, config.SyncConfig{})

	now := time.Now()
	mocks["cal1"].AddEvents("c1", provider.Event{
		ID: "busy1", CalendarID: "c1", Title: "Busy",
		Start: now.Add(1 * time.Hour), End: now.Add(2 * time.Hour), IsBusy: true,
	})

	ctx := context.Background()
	if err := engine.Run(ctx); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if len(mocks["cal2"].CreatedEvents) != 0 {
		t.Errorf("events matching blocker_title should be skipped, got %d blockers", len(mocks["cal2"].CreatedEvents))
	}
}

func TestEngine_SourceTaggedEventsSkipped(t *testing.T) {
	cals := []config.CalendarConfig{
		{ID: "cal1", Provider: "google", CalendarID: "c1"},
		{ID: "cal2", Provider: "outlook", CalendarID: "c2"},
	}

	engine, mocks, _ := newTestEngine(t, cals, config.SyncConfig{})

	now := time.Now()
	// This is a blocker we previously created (has a SourceTag)
	mocks["cal1"].AddEvents("c1", provider.Event{
		ID: "b1", CalendarID: "c1", Title: "Busy",
		Start: now.Add(1 * time.Hour), End: now.Add(2 * time.Hour),
		IsBusy: true, SourceTag: "cal2:c2:ev99",
	})

	ctx := context.Background()
	if err := engine.Run(ctx); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Source-tagged events are our own blockers — should not cascade
	if len(mocks["cal2"].CreatedEvents) != 0 {
		t.Errorf("source-tagged events should not create blockers, got %d", len(mocks["cal2"].CreatedEvents))
	}
}

func TestEngine_DryRun(t *testing.T) {
	cals := []config.CalendarConfig{
		{ID: "cal1", Provider: "google", CalendarID: "c1"},
		{ID: "cal2", Provider: "outlook", CalendarID: "c2"},
	}

	syncCfg := config.SyncConfig{DryRun: true}
	engine, mocks, _ := newTestEngine(t, cals, syncCfg)

	now := time.Now()
	mocks["cal1"].AddEvents("c1", provider.Event{
		ID: "ev1", CalendarID: "c1", Title: "Real Meeting",
		Start: now.Add(1 * time.Hour), End: now.Add(2 * time.Hour), IsBusy: true,
	})

	ctx := context.Background()
	if err := engine.Run(ctx); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Dry-run should NOT create any events
	if len(mocks["cal2"].CreatedEvents) != 0 {
		t.Errorf("dry-run should not create events, got %d", len(mocks["cal2"].CreatedEvents))
	}
}

func TestEngine_OrphanedBlockerCleanup(t *testing.T) {
	cals := []config.CalendarConfig{
		{ID: "cal1", Provider: "google", CalendarID: "c1"},
		{ID: "cal2", Provider: "outlook", CalendarID: "c2"},
	}

	engine, mocks, state := newTestEngine(t, cals, config.SyncConfig{})

	now := time.Now()
	mocks["cal1"].AddEvents("c1", provider.Event{
		ID: "ev1", CalendarID: "c1", Title: "Meeting",
		Start: now.Add(1 * time.Hour), End: now.Add(2 * time.Hour), IsBusy: true,
	})

	ctx := context.Background()

	// First run — creates blocker
	if err := engine.Run(ctx); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(mocks["cal2"].CreatedEvents) != 1 {
		t.Fatalf("expected 1 blocker created, got %d", len(mocks["cal2"].CreatedEvents))
	}

	// Now simulate the source event being deleted: clear events from cal1
	// but the mock still returns whatever is in its events list for the time range,
	// so we need to recreate the mock with no events
	emptyMock := provider.NewMockProvider("google")
	engine.providers["cal1"] = emptyMock

	// Second run — should detect orphaned blocker and delete it
	mocks["cal2"].Reset()
	if err := engine.Run(ctx); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if len(mocks["cal2"].DeletedEvents) != 1 {
		t.Errorf("expected 1 orphaned blocker deleted, got %d", len(mocks["cal2"].DeletedEvents))
	}

	// State should be cleaned up
	sourceTag := provider.MakeSourceTag("cal1", "c1", "ev1")
	if blockers := state.GetBlockers(sourceTag); len(blockers) != 0 {
		t.Errorf("state should have 0 blockers after cleanup, got %d", len(blockers))
	}
}

func TestEngine_MultipleEventsMultipleCalendars(t *testing.T) {
	cals := []config.CalendarConfig{
		{ID: "cal1", Provider: "google", CalendarID: "c1"},
		{ID: "cal2", Provider: "outlook", CalendarID: "c2"},
		{ID: "cal3", Provider: "apple", CalendarID: "c3"},
	}

	engine, mocks, _ := newTestEngine(t, cals, config.SyncConfig{})

	now := time.Now()
	// Event on cal1
	mocks["cal1"].AddEvents("c1", provider.Event{
		ID: "ev1", CalendarID: "c1", Title: "Meeting A",
		Start: now.Add(1 * time.Hour), End: now.Add(2 * time.Hour), IsBusy: true,
	})
	// Event on cal2
	mocks["cal2"].AddEvents("c2", provider.Event{
		ID: "ev2", CalendarID: "c2", Title: "Meeting B",
		Start: now.Add(3 * time.Hour), End: now.Add(4 * time.Hour), IsBusy: true,
	})

	ctx := context.Background()
	if err := engine.Run(ctx); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// ev1 (from cal1) should create blockers on cal2 and cal3
	// ev2 (from cal2) should create blockers on cal1 and cal3
	// Total: cal1 gets 1, cal2 gets 1, cal3 gets 2
	if len(mocks["cal1"].CreatedEvents) != 1 {
		t.Errorf("cal1 should have 1 blocker (from ev2), got %d", len(mocks["cal1"].CreatedEvents))
	}
	if len(mocks["cal2"].CreatedEvents) != 1 {
		t.Errorf("cal2 should have 1 blocker (from ev1), got %d", len(mocks["cal2"].CreatedEvents))
	}
	if len(mocks["cal3"].CreatedEvents) != 2 {
		t.Errorf("cal3 should have 2 blockers (from ev1 + ev2), got %d", len(mocks["cal3"].CreatedEvents))
	}
}

func TestEngine_AllDayEventsHandled(t *testing.T) {
	cals := []config.CalendarConfig{
		{ID: "cal1", Provider: "google", CalendarID: "c1"},
		{ID: "cal2", Provider: "outlook", CalendarID: "c2"},
	}

	engine, mocks, _ := newTestEngine(t, cals, config.SyncConfig{})

	today := time.Now().Truncate(24 * time.Hour)
	mocks["cal1"].AddEvents("c1", provider.Event{
		ID: "allday1", CalendarID: "c1", Title: "Company Holiday",
		Start: today, End: today.Add(24 * time.Hour),
		IsAllDay: true, IsBusy: true,
	})

	ctx := context.Background()
	if err := engine.Run(ctx); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if len(mocks["cal2"].CreatedEvents) != 1 {
		t.Fatalf("all-day event should create blocker, got %d", len(mocks["cal2"].CreatedEvents))
	}

	blocker := mocks["cal2"].CreatedEvents[0].Event
	if !blocker.IsAllDay {
		t.Error("blocker for all-day event should also be all-day")
	}
}
