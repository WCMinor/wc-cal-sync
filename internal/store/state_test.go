package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestState_LoadSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := New(path)

	// Load non-existent file should be fine (fresh start)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() on missing file: %v", err)
	}

	// Set some state
	s.SetSyncToken("cal1", "token-abc")
	s.AddBlocker("src:cal1:ev1", BlockerRef{
		CalendarConfigID: "cal2",
		CalendarID:       "cal2-id",
		EventID:          "blocker1",
	})
	s.SetMergedEventID("src:cal1:ev1", "merged1")

	// Save
	if err := s.Save(); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not created: %v", err)
	}

	// Load into new instance
	s2 := New(path)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load(): %v", err)
	}

	if got := s2.GetSyncToken("cal1"); got != "token-abc" {
		t.Errorf("GetSyncToken = %q, want %q", got, "token-abc")
	}

	blockers := s2.GetBlockers("src:cal1:ev1")
	if len(blockers) != 1 {
		t.Fatalf("GetBlockers len = %d, want 1", len(blockers))
	}
	if blockers[0].EventID != "blocker1" {
		t.Errorf("blocker EventID = %q, want %q", blockers[0].EventID, "blocker1")
	}

	if got := s2.GetMergedEventID("src:cal1:ev1"); got != "merged1" {
		t.Errorf("GetMergedEventID = %q, want %q", got, "merged1")
	}
}

func TestState_BlockerOperations(t *testing.T) {
	s := New("")
	// Don't load from file, just use in-memory

	sourceTag := "prov1:cal1:ev1"

	// Initially empty
	if got := s.GetBlockers(sourceTag); len(got) != 0 {
		t.Errorf("initial GetBlockers len = %d, want 0", len(got))
	}

	// Add blockers on two calendars
	s.AddBlocker(sourceTag, BlockerRef{CalendarConfigID: "cal2", CalendarID: "cal2-id", EventID: "b1"})
	s.AddBlocker(sourceTag, BlockerRef{CalendarConfigID: "cal3", CalendarID: "cal3-id", EventID: "b2"})

	if got := s.GetBlockers(sourceTag); len(got) != 2 {
		t.Fatalf("after adds, GetBlockers len = %d, want 2", len(got))
	}

	// Remove blocker for cal2 only
	s.RemoveBlocker(sourceTag, "cal2")
	blockers := s.GetBlockers(sourceTag)
	if len(blockers) != 1 {
		t.Fatalf("after RemoveBlocker(cal2), len = %d, want 1", len(blockers))
	}
	if blockers[0].CalendarConfigID != "cal3" {
		t.Errorf("remaining blocker = %q, want cal3", blockers[0].CalendarConfigID)
	}

	// Remove all
	s.RemoveBlockers(sourceTag)
	if got := s.GetBlockers(sourceTag); len(got) != 0 {
		t.Errorf("after RemoveBlockers, len = %d, want 0", len(got))
	}
}

func TestState_MergedEventOperations(t *testing.T) {
	s := New("")

	sourceTag := "prov1:cal1:ev1"

	if got := s.GetMergedEventID(sourceTag); got != "" {
		t.Errorf("initial GetMergedEventID = %q, want empty", got)
	}

	s.SetMergedEventID(sourceTag, "merged-123")
	if got := s.GetMergedEventID(sourceTag); got != "merged-123" {
		t.Errorf("GetMergedEventID = %q, want %q", got, "merged-123")
	}

	s.RemoveMergedEvent(sourceTag)
	if got := s.GetMergedEventID(sourceTag); got != "" {
		t.Errorf("after remove, GetMergedEventID = %q, want empty", got)
	}
}

func TestState_AllTags(t *testing.T) {
	s := New("")

	s.AddBlocker("tag1", BlockerRef{CalendarConfigID: "c1", CalendarID: "c1-id", EventID: "b1"})
	s.AddBlocker("tag2", BlockerRef{CalendarConfigID: "c2", CalendarID: "c2-id", EventID: "b2"})
	s.SetMergedEventID("tag1", "m1")
	s.SetMergedEventID("tag3", "m3")

	blockerTags := s.AllBlockerSourceTags()
	if len(blockerTags) != 2 {
		t.Errorf("AllBlockerSourceTags len = %d, want 2", len(blockerTags))
	}

	mergedTags := s.AllMergedSourceTags()
	if len(mergedTags) != 2 {
		t.Errorf("AllMergedSourceTags len = %d, want 2", len(mergedTags))
	}
}
