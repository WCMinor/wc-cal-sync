package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/user/wc-cal-sync/internal/config"
	"github.com/user/wc-cal-sync/internal/oauth"
	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"
)

type GoogleProvider struct {
	client *http.Client
}

const googleCalendarBaseURL = "https://www.googleapis.com/calendar/v3"

func NewGoogleProvider(ctx context.Context, cal config.CalendarConfig, callbackPort int) (*GoogleProvider, error) {
	oauthCfg := &oauth2.Config{
		ClientID:     cal.GoogleClientID,
		ClientSecret: cal.GoogleClientSecret,
		Endpoint:     googleoauth.Endpoint,
		Scopes:       []string{"https://www.googleapis.com/auth/calendar.events"},
	}

	tokenFile := cal.TokenFile
	if tokenFile == "" {
		tokenFile = fmt.Sprintf("tokens/google-%s.json", cal.ID)
	}

	client, err := oauth.GetClient(ctx, oauthCfg, tokenFile, callbackPort)
	if err != nil {
		return nil, fmt.Errorf("google oauth for %s: %w", cal.ID, err)
	}

	return &GoogleProvider{client: client}, nil
}

func NewGoogleProviderFromMerged(ctx context.Context, cfg config.MergedCalendarConfig, callbackPort int) (*GoogleProvider, error) {
	cal := config.CalendarConfig{
		ID:                 "merged",
		GoogleClientID:     cfg.GoogleClientID,
		GoogleClientSecret: cfg.GoogleClientSecret,
		TokenFile:          cfg.TokenFile,
	}
	return NewGoogleProvider(ctx, cal, callbackPort)
}

func (g *GoogleProvider) Name() string { return "google" }

func (g *GoogleProvider) ListEvents(ctx context.Context, calendarID string, start, end time.Time, syncToken string) (*ListResult, error) {
	params := url.Values{
		"singleEvents": {"true"},
		"orderBy":      {"startTime"},
		"showDeleted":  {"true"},
		"maxResults":   {"2500"},
	}

	if syncToken != "" {
		params.Set("syncToken", syncToken)
	} else {
		params.Set("timeMin", start.Format(time.RFC3339))
		params.Set("timeMax", end.Format(time.RFC3339))
	}

	var allEvents []Event
	var nextSyncToken string
	pageToken := ""

	for {
		p := url.Values{}
		for k, v := range params {
			p[k] = v
		}
		if pageToken != "" {
			p.Set("pageToken", pageToken)
		}

		reqURL := fmt.Sprintf("%s/calendars/%s/events?%s", googleCalendarBaseURL, url.PathEscape(calendarID), p.Encode())
		req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err != nil {
			return nil, err
		}

		resp, err := g.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("google api request: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == 410 && syncToken != "" {
			log.Printf("Google sync token expired for %s, doing full sync", calendarID)
			return g.ListEvents(ctx, calendarID, start, end, "")
		}

		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("google api error %d: %s", resp.StatusCode, string(body))
		}

		var result googleEventsResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("parsing google response: %w", err)
		}

		for _, item := range result.Items {
			if item.Status == "cancelled" {
				allEvents = append(allEvents, Event{ID: item.ID, CalendarID: calendarID})
				continue
			}
			ev, err := googleItemToEvent(&item, calendarID)
			if err != nil {
				log.Printf("skipping google event %s: %v", item.ID, err)
				continue
			}
			allEvents = append(allEvents, *ev)
		}

		nextSyncToken = result.NextSyncToken
		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}

	return &ListResult{Events: allEvents, NextSyncToken: nextSyncToken}, nil
}

func (g *GoogleProvider) CreateEvent(ctx context.Context, calendarID string, event *Event) error {
	gEvent := eventToGoogleJSON(event)
	body, err := json.Marshal(gEvent)
	if err != nil {
		return err
	}

	reqURL := fmt.Sprintf("%s/calendars/%s/events", googleCalendarBaseURL, url.PathEscape(calendarID))
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("creating google event: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("create event failed %d: %s", resp.StatusCode, string(respBody))
	}

	var created googleEventItem
	if err := json.Unmarshal(respBody, &created); err != nil {
		return err
	}
	event.ID = created.ID
	return nil
}

func (g *GoogleProvider) UpdateEvent(ctx context.Context, calendarID string, event *Event) error {
	gEvent := eventToGoogleJSON(event)
	body, err := json.Marshal(gEvent)
	if err != nil {
		return err
	}

	reqURL := fmt.Sprintf("%s/calendars/%s/events/%s", googleCalendarBaseURL, url.PathEscape(calendarID), url.PathEscape(event.ID))
	req, err := http.NewRequestWithContext(ctx, "PUT", reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("updating google event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update event failed %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (g *GoogleProvider) DeleteEvent(ctx context.Context, calendarID string, eventID string) error {
	reqURL := fmt.Sprintf("%s/calendars/%s/events/%s", googleCalendarBaseURL, url.PathEscape(calendarID), url.PathEscape(eventID))
	req, err := http.NewRequestWithContext(ctx, "DELETE", reqURL, nil)
	if err != nil {
		return err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("deleting google event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 && resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete event failed %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// Google Calendar API JSON types

type googleEventsResponse struct {
	Items         []googleEventItem `json:"items"`
	NextPageToken string            `json:"nextPageToken"`
	NextSyncToken string            `json:"nextSyncToken"`
}

type googleEventItem struct {
	ID                 string                     `json:"id"`
	Status             string                     `json:"status"`
	Summary            string                     `json:"summary"`
	Location           string                     `json:"location"`
	Description        string                     `json:"description"`
	Start              *googleEventDateTime       `json:"start"`
	End                *googleEventDateTime       `json:"end"`
	Transparency       string                     `json:"transparency"`
	Etag               string                     `json:"etag"`
	ExtendedProperties *googleExtendedProperties  `json:"extendedProperties,omitempty"`
}

type googleEventDateTime struct {
	DateTime string `json:"dateTime,omitempty"`
	Date     string `json:"date,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

type googleExtendedProperties struct {
	Private map[string]string `json:"private,omitempty"`
}

func googleItemToEvent(item *googleEventItem, calendarID string) (*Event, error) {
	ev := &Event{
		ID:          item.ID,
		CalendarID:  calendarID,
		Title:       item.Summary,
		Location:    item.Location,
		Description: item.Description,
		IsBusy:      item.Transparency != "transparent",
		ETag:        item.Etag,
	}

	if item.Start == nil {
		return nil, fmt.Errorf("event has no start time")
	}

	if item.Start.DateTime != "" {
		t, err := time.Parse(time.RFC3339, item.Start.DateTime)
		if err != nil {
			return nil, fmt.Errorf("parsing start: %w", err)
		}
		ev.Start = t
	} else if item.Start.Date != "" {
		t, err := time.Parse("2006-01-02", item.Start.Date)
		if err != nil {
			return nil, fmt.Errorf("parsing start date: %w", err)
		}
		ev.Start = t
		ev.IsAllDay = true
	}

	if item.End != nil {
		if item.End.DateTime != "" {
			t, err := time.Parse(time.RFC3339, item.End.DateTime)
			if err != nil {
				return nil, fmt.Errorf("parsing end: %w", err)
			}
			ev.End = t
		} else if item.End.Date != "" {
			t, err := time.Parse("2006-01-02", item.End.Date)
			if err != nil {
				return nil, fmt.Errorf("parsing end date: %w", err)
			}
			ev.End = t
		}
	}

	if item.ExtendedProperties != nil && item.ExtendedProperties.Private != nil {
		if tag, ok := item.ExtendedProperties.Private[SourceTagProperty]; ok {
			ev.SourceTag = tag
		}
	}

	return ev, nil
}

func eventToGoogleJSON(event *Event) *googleEventItem {
	item := &googleEventItem{
		Summary:     event.Title,
		Location:    event.Location,
		Description: event.Description,
	}

	if event.IsAllDay {
		item.Start = &googleEventDateTime{Date: event.Start.Format("2006-01-02")}
		item.End = &googleEventDateTime{Date: event.End.Format("2006-01-02")}
	} else {
		item.Start = &googleEventDateTime{DateTime: event.Start.Format(time.RFC3339)}
		item.End = &googleEventDateTime{DateTime: event.End.Format(time.RFC3339)}
	}

	if !event.IsBusy {
		item.Transparency = "transparent"
	}

	if event.SourceTag != "" {
		item.ExtendedProperties = &googleExtendedProperties{
			Private: map[string]string{
				SourceTagProperty: event.SourceTag,
			},
		}
	}

	return item
}

func init() {
	// Ensure strings is used (for isGoogleSyncTokenInvalid)
	_ = strings.Contains
}
