package provider

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MockProvider is a test double for Provider that records all operations.
type MockProvider struct {
	mu     sync.Mutex
	name   string
	events map[string][]Event // calendarID -> events

	// Recorded operations
	CreatedEvents []MockCreateCall
	UpdatedEvents []MockUpdateCall
	DeletedEvents []MockDeleteCall
	ListCalls     []MockListCall
}

type MockCreateCall struct {
	CalendarID string
	Event      Event
}

type MockUpdateCall struct {
	CalendarID string
	Event      Event
}

type MockDeleteCall struct {
	CalendarID string
	EventID    string
}

type MockListCall struct {
	CalendarID string
	Start      time.Time
	End        time.Time
	SyncToken  string
}

func NewMockProvider(name string) *MockProvider {
	return &MockProvider{
		name:   name,
		events: make(map[string][]Event),
	}
}

// AddEvents pre-populates events for ListEvents to return.
func (m *MockProvider) AddEvents(calendarID string, events ...Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events[calendarID] = append(m.events[calendarID], events...)
}

func (m *MockProvider) Name() string { return m.name }

func (m *MockProvider) ListEvents(_ context.Context, calendarID string, start, end time.Time, syncToken string) (*ListResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ListCalls = append(m.ListCalls, MockListCall{
		CalendarID: calendarID,
		Start:      start,
		End:        end,
		SyncToken:  syncToken,
	})
	events := m.events[calendarID]
	// Filter to time range
	var filtered []Event
	for _, ev := range events {
		if ev.End.After(start) && ev.Start.Before(end) {
			filtered = append(filtered, ev)
		}
	}
	return &ListResult{Events: filtered, NextSyncToken: "mock-sync-token"}, nil
}

func (m *MockProvider) CreateEvent(_ context.Context, calendarID string, event *Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	event.ID = fmt.Sprintf("mock-%s-%d", calendarID, len(m.CreatedEvents))
	m.CreatedEvents = append(m.CreatedEvents, MockCreateCall{
		CalendarID: calendarID,
		Event:      *event,
	})
	return nil
}

func (m *MockProvider) UpdateEvent(_ context.Context, calendarID string, event *Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.UpdatedEvents = append(m.UpdatedEvents, MockUpdateCall{
		CalendarID: calendarID,
		Event:      *event,
	})
	return nil
}

func (m *MockProvider) DeleteEvent(_ context.Context, calendarID string, eventID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DeletedEvents = append(m.DeletedEvents, MockDeleteCall{
		CalendarID: calendarID,
		EventID:    eventID,
	})
	return nil
}

// Reset clears all recorded operations.
func (m *MockProvider) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CreatedEvents = nil
	m.UpdatedEvents = nil
	m.DeletedEvents = nil
	m.ListCalls = nil
}
