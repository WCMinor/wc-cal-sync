package calendar_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ical "github.com/arran4/golang-ical"

	"github.com/WCMinor/wc-cal-sync/internal/calendar"
)

// minimalICS returns a minimal valid iCalendar string with the given events.
// Each entry in events should be the full VEVENT block content (without
// BEGIN:VEVENT / END:VEVENT).
func minimalICS(events ...string) string {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//Test//EN\r\n")
	for _, e := range events {
		b.WriteString("BEGIN:VEVENT\r\n")
		b.WriteString(e)
		if !strings.HasSuffix(e, "\r\n") {
			b.WriteString("\r\n")
		}
		b.WriteString("END:VEVENT\r\n")
	}
	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

const event1 = "UID:event-001\r\nSUMMARY:Meeting\r\nDTSTART:20260101T090000Z\r\nDTEND:20260101T100000Z\r\n"
const event2 = "UID:event-002\r\nSUMMARY:Lunch\r\nDTSTART:20260101T120000Z\r\nDTEND:20260101T130000Z\r\n"
const event1Dup = "UID:event-001\r\nSUMMARY:Meeting (duplicate)\r\nDTSTART:20260101T090000Z\r\nDTEND:20260101T100000Z\r\n"

func parseICS(t *testing.T, s string) *ical.Calendar {
	t.Helper()
	cal, err := ical.ParseCalendar(strings.NewReader(s))
	if err != nil {
		t.Fatalf("ParseCalendar: %v", err)
	}
	return cal
}

// --- Fetcher tests ---

func TestFetcher_FetchHTTP_Success(t *testing.T) {
	icsData := minimalICS(event1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		w.Write([]byte(icsData))
	}))
	defer srv.Close()

	f := calendar.NewFetcher(5 * time.Second)
	cal, err := f.Fetch(context.Background(), srv.URL) 
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(cal.Events()) != 1 {
		t.Errorf("expected 1 event, got %d", len(cal.Events()))
	}
}

func TestFetcher_FetchHTTP_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	f := calendar.NewFetcher(5 * time.Second)
	_, err := f.Fetch(context.Background(), srv.URL) 
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestFetcher_FetchFile_Success(t *testing.T) {
	dir := t.TempDir()
	icsPath := filepath.Join(dir, "test.ics")
	if err := os.WriteFile(icsPath, []byte(minimalICS(event1, event2)), 0o644); err != nil {
		t.Fatal(err)
	}

	f := calendar.NewFetcher(5 * time.Second)
	cal, err := f.Fetch(context.Background(), icsPath) 
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(cal.Events()) != 2 {
		t.Errorf("expected 2 events, got %d", len(cal.Events()))
	}
}

func TestFetcher_FetchFile_NotFound(t *testing.T) {
	f := calendar.NewFetcher(5 * time.Second)
	_, err := f.Fetch(context.Background(), "/nonexistent/path/to/calendar.ics") 
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// --- Merge tests ---

func TestMerge_CombinesEvents(t *testing.T) {
	cal1 := parseICS(t, minimalICS(event1))
	cal2 := parseICS(t, minimalICS(event2))

	merged := calendar.Merge([]*ical.Calendar{cal1, cal2})
	if len(merged.Events()) != 2 {
		t.Errorf("expected 2 events after merge, got %d", len(merged.Events()))
	}
}

func TestMerge_DeduplicatesByUID(t *testing.T) {
	cal1 := parseICS(t, minimalICS(event1))
	cal2 := parseICS(t, minimalICS(event1Dup, event2))

	merged := calendar.Merge([]*ical.Calendar{cal1, cal2})
	events := merged.Events()
	if len(events) != 2 {
		t.Errorf("expected 2 events (deduplicated), got %d", len(events))
	}
	// The first occurrence should win.
	for _, ev := range events {
		if ev.GetProperty(ical.ComponentPropertyUniqueId) != nil &&
			ev.GetProperty(ical.ComponentPropertyUniqueId).Value == "event-001" {
			summary := ev.GetProperty(ical.ComponentPropertySummary)
			if summary == nil || summary.Value != "Meeting" {
				t.Errorf("expected original event-001 to be kept, got summary=%v", summary)
			}
		}
	}
}

func TestMerge_NilCalendarsSkipped(t *testing.T) {
	cal1 := parseICS(t, minimalICS(event1))
	merged := calendar.Merge([]*ical.Calendar{nil, cal1, nil})
	if len(merged.Events()) != 1 {
		t.Errorf("expected 1 event, got %d", len(merged.Events()))
	}
}

func TestMerge_EmptyList(t *testing.T) {
	merged := calendar.Merge(nil)
	if merged == nil {
		t.Fatal("expected non-nil calendar")
	}
	if len(merged.Events()) != 0 {
		t.Errorf("expected 0 events, got %d", len(merged.Events()))
	}
}

// --- Write tests ---

func TestWrite_ProducesValidICS(t *testing.T) {
	cal := parseICS(t, minimalICS(event1))
	var buf bytes.Buffer
	if err := calendar.Write(&buf, cal); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "BEGIN:VCALENDAR") {
		t.Error("output missing BEGIN:VCALENDAR")
	}
	if !strings.Contains(out, "BEGIN:VEVENT") {
		t.Error("output missing BEGIN:VEVENT")
	}
}

func TestWriteFile_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.ics")

	cal := parseICS(t, minimalICS(event1, event2))
	if err := calendar.WriteFile(outPath, cal); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "event-001") {
		t.Error("output file missing event-001 UID")
	}
	if !strings.Contains(string(data), "event-002") {
		t.Error("output file missing event-002 UID")
	}
}

func TestWriteFile_InvalidPath(t *testing.T) {
	cal := parseICS(t, minimalICS(event1))
	err := calendar.WriteFile("/nonexistent/dir/out.ics", cal)
	if err == nil {
		t.Fatal("expected error for invalid output path")
	}
}
