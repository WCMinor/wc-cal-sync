// Package calendar provides functionality for fetching, parsing, merging, and
// writing iCalendar (.ics) data.
package calendar

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	ical "github.com/arran4/golang-ical"
)

// Fetcher retrieves raw iCalendar data from a URL (http/https) or a local file
// path and returns the parsed calendar.
type Fetcher struct {
	client *http.Client
}

// NewFetcher creates a Fetcher that enforces the given HTTP timeout.
func NewFetcher(timeout time.Duration) *Fetcher {
	return &Fetcher{
		client: &http.Client{Timeout: timeout},
	}
}

// Fetch retrieves an iCalendar from src (an HTTP/HTTPS URL or a local file path).
func (f *Fetcher) Fetch(ctx context.Context, src string) (*ical.Calendar, error) {
	if isHTTP(src) {
		return f.fetchHTTP(ctx, src)
	}
	return fetchFile(src)
}

func isHTTP(src string) bool {
	u, err := url.Parse(src)
	if err != nil {
		return false
	}
	s := strings.ToLower(u.Scheme)
	return s == "http" || s == "https"
}

func (f *Fetcher) fetchHTTP(ctx context.Context, rawURL string) (*ical.Calendar, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %q: %w", rawURL, err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %q: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %q: unexpected status %s", rawURL, resp.Status)
	}

	cal, err := ical.ParseCalendar(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parsing calendar from %q: %w", rawURL, err)
	}
	return cal, nil
}

func fetchFile(path string) (*ical.Calendar, error) {
	f, err := os.Open(path) // #nosec G304 -- path is user-supplied config, not attacker-controlled
	if err != nil {
		return nil, fmt.Errorf("opening file %q: %w", path, err)
	}
	defer f.Close()

	cal, err := ical.ParseCalendar(f)
	if err != nil {
		return nil, fmt.Errorf("parsing calendar from %q: %w", path, err)
	}
	return cal, nil
}

// Merge combines events from all provided calendars into a single new calendar.
// The resulting calendar retains the PRODID and VERSION of the first non-nil
// source, and deduplicates events by UID (first occurrence wins).
func Merge(cals []*ical.Calendar) *ical.Calendar {
	merged := ical.NewCalendar()
	seen := make(map[string]struct{})

	for _, cal := range cals {
		if cal == nil {
			continue
		}

		// Copy top-level properties from the first source calendar.
		if len(merged.CalendarProperties) == 0 {
			merged.CalendarProperties = append(merged.CalendarProperties, cal.CalendarProperties...)
		}

		for _, component := range cal.Components {
			uid := componentUID(component)
			if uid == "" {
				// No UID – always include.
				merged.Components = append(merged.Components, component)
				continue
			}
			if _, exists := seen[uid]; !exists {
				seen[uid] = struct{}{}
				merged.Components = append(merged.Components, component)
			}
		}
	}

	return merged
}

// componentUID returns the UID property value of an iCalendar component,
// or an empty string if it has none.
func componentUID(c ical.Component) string {
	type uidder interface {
		GetProperty(ical.ComponentProperty) *ical.IANAProperty
	}
	if u, ok := c.(uidder); ok {
		prop := u.GetProperty(ical.ComponentPropertyUniqueId)
		if prop != nil {
			return prop.Value
		}
	}
	return ""
}

// Write serialises cal to w in iCalendar format.
func Write(w io.Writer, cal *ical.Calendar) error {
	return cal.SerializeTo(w)
}

// WriteFile serialises cal and writes it to the file at path, creating or
// truncating the file as needed.
func WriteFile(path string, cal *ical.Calendar) error {
	f, err := os.Create(path) // #nosec G304 -- path is user-supplied config
	if err != nil {
		return fmt.Errorf("creating output file %q: %w", path, err)
	}
	defer f.Close()

	if err := Write(f, cal); err != nil {
		return fmt.Errorf("writing calendar to %q: %w", path, err)
	}
	return nil
}
