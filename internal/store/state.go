package store

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// State persists sync tokens and blocker mappings between runs.
type State struct {
	mu   sync.RWMutex
	path string
	data StateData
}

type StateData struct {
	// SyncTokens maps "calendarConfigID" -> sync token (Google syncToken, Outlook deltaLink, etc.)
	SyncTokens map[string]string `json:"sync_tokens"`

	// Blockers maps "sourceTag" -> list of created blockers
	// sourceTag = "providerID:calendarID:eventID"
	// Each blocker records where it was created
	Blockers map[string][]BlockerRef `json:"blockers"`

	// MergedEvents maps "sourceTag" -> merged event ID on the merged calendar
	MergedEvents map[string]string `json:"merged_events"`
}

// BlockerRef identifies a blocker event on a specific calendar.
type BlockerRef struct {
	CalendarConfigID string `json:"calendar_config_id"`
	CalendarID       string `json:"calendar_id"`
	EventID          string `json:"event_id"`
}

func New(path string) *State {
	return &State{
		path: path,
		data: StateData{
			SyncTokens:   make(map[string]string),
			Blockers:     make(map[string][]BlockerRef),
			MergedEvents: make(map[string]string),
		},
	}
}

func (s *State) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // fresh start
		}
		return fmt.Errorf("reading state: %w", err)
	}

	if err := json.Unmarshal(data, &s.data); err != nil {
		return fmt.Errorf("parsing state: %w", err)
	}

	// Ensure maps are initialized
	if s.data.SyncTokens == nil {
		s.data.SyncTokens = make(map[string]string)
	}
	if s.data.Blockers == nil {
		s.data.Blockers = make(map[string][]BlockerRef)
	}
	if s.data.MergedEvents == nil {
		s.data.MergedEvents = make(map[string]string)
	}

	return nil
}

func (s *State) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	return os.WriteFile(s.path, data, 0600)
}

func (s *State) GetSyncToken(calendarID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.SyncTokens[calendarID]
}

func (s *State) SetSyncToken(calendarID, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.SyncTokens[calendarID] = token
}

func (s *State) GetBlockers(sourceTag string) []BlockerRef {
	s.mu.RLock()
	defer s.mu.RUnlock()
	refs := s.data.Blockers[sourceTag]
	// Return a copy
	result := make([]BlockerRef, len(refs))
	copy(result, refs)
	return result
}

func (s *State) AddBlocker(sourceTag string, ref BlockerRef) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Blockers[sourceTag] = append(s.data.Blockers[sourceTag], ref)
}

func (s *State) RemoveBlockers(sourceTag string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data.Blockers, sourceTag)
}

func (s *State) RemoveBlocker(sourceTag string, calConfigID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	refs := s.data.Blockers[sourceTag]
	filtered := refs[:0]
	for _, r := range refs {
		if r.CalendarConfigID != calConfigID {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) == 0 {
		delete(s.data.Blockers, sourceTag)
	} else {
		s.data.Blockers[sourceTag] = filtered
	}
}

func (s *State) GetMergedEventID(sourceTag string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.MergedEvents[sourceTag]
}

func (s *State) SetMergedEventID(sourceTag, eventID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.MergedEvents[sourceTag] = eventID
}

func (s *State) RemoveMergedEvent(sourceTag string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data.MergedEvents, sourceTag)
}

// AllBlockerSourceTags returns all source tags that have blockers.
func (s *State) AllBlockerSourceTags() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tags := make([]string, 0, len(s.data.Blockers))
	for tag := range s.data.Blockers {
		tags = append(tags, tag)
	}
	return tags
}

// AllMergedSourceTags returns all source tags that have merged events.
func (s *State) AllMergedSourceTags() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tags := make([]string, 0, len(s.data.MergedEvents))
	for tag := range s.data.MergedEvents {
		tags = append(tags, tag)
	}
	return tags
}
