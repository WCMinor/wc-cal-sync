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
	"time"

	"github.com/user/wc-cal-sync/internal/config"
	"github.com/user/wc-cal-sync/internal/oauth"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/microsoft"
)

type OutlookProvider struct {
	client *http.Client
}

const graphBaseURL = "https://graph.microsoft.com/v1.0"

func NewOutlookProvider(ctx context.Context, cal config.CalendarConfig, callbackPort int) (*OutlookProvider, error) {
	endpoint := microsoft.AzureADEndpoint(cal.TenantID)

	oauthCfg := &oauth2.Config{
		ClientID:     cal.ClientID,
		ClientSecret: cal.ClientSecret,
		Endpoint:     endpoint,
		Scopes:       []string{"Calendars.ReadWrite", "offline_access"},
	}

	tokenFile := cal.TokenFile
	if tokenFile == "" {
		tokenFile = fmt.Sprintf("tokens/outlook-%s.json", cal.ID)
	}

	client, err := oauth.GetClient(ctx, oauthCfg, tokenFile, callbackPort)
	if err != nil {
		return nil, fmt.Errorf("outlook oauth for %s: %w", cal.ID, err)
	}

	return &OutlookProvider{client: client}, nil
}

// NewOutlookProviderFromMerged creates an Outlook provider from merged calendar config.
func NewOutlookProviderFromMerged(ctx context.Context, cfg config.MergedCalendarConfig, callbackPort int) (*OutlookProvider, error) {
	cal := config.CalendarConfig{
		ID:           "merged",
		TenantID:     cfg.TenantID,
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		TokenFile:    cfg.TokenFile,
	}
	return NewOutlookProvider(ctx, cal, callbackPort)
}

func (o *OutlookProvider) Name() string { return "outlook" }

func (o *OutlookProvider) ListEvents(ctx context.Context, calendarID string, start, end time.Time, syncToken string) (*ListResult, error) {
	var reqURL string

	if syncToken != "" {
		// Delta query with token
		reqURL = syncToken
	} else {
		// Calendar view with time range
		params := url.Values{
			"startDateTime": {start.UTC().Format(time.RFC3339)},
			"endDateTime":   {end.UTC().Format(time.RFC3339)},
			"$top":          {"250"},
			"$select":       {"id,subject,start,end,location,bodyPreview,showAs,isAllDay,singleValueExtendedProperties"},
			"$expand":       {fmt.Sprintf("singleValueExtendedProperties($filter=id eq 'String {00020329-0000-0000-C000-000000000046} Name %s')", SourceTagProperty)},
		}

		if calendarID == "" || calendarID == "primary" {
			reqURL = fmt.Sprintf("%s/me/calendarView?%s", graphBaseURL, params.Encode())
		} else {
			reqURL = fmt.Sprintf("%s/me/calendars/%s/calendarView?%s", graphBaseURL, calendarID, params.Encode())
		}
	}

	var events []Event
	var nextLink string

	for reqURL != "" {
		req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Prefer", "odata.maxpagesize=250")

		resp, err := o.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("graph api request: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("graph api error %d: %s", resp.StatusCode, string(body))
		}

		var result graphEventsResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("parsing graph response: %w", err)
		}

		for _, item := range result.Value {
			ev := outlookEventToEvent(&item, calendarID)
			events = append(events, *ev)
		}

		reqURL = result.NextLink
		if result.DeltaLink != "" {
			nextLink = result.DeltaLink
		}
	}

	return &ListResult{Events: events, NextSyncToken: nextLink}, nil
}

func (o *OutlookProvider) CreateEvent(ctx context.Context, calendarID string, event *Event) error {
	gEvent := eventToOutlookEvent(event)

	body, err := json.Marshal(gEvent)
	if err != nil {
		return err
	}

	var reqURL string
	if calendarID == "" || calendarID == "primary" {
		reqURL = fmt.Sprintf("%s/me/events", graphBaseURL)
	} else {
		reqURL = fmt.Sprintf("%s/me/calendars/%s/events", graphBaseURL, calendarID)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("creating outlook event: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 201 {
		return fmt.Errorf("create event failed %d: %s", resp.StatusCode, string(respBody))
	}

	var created graphEvent
	if err := json.Unmarshal(respBody, &created); err != nil {
		return err
	}
	event.ID = created.ID

	return nil
}

func (o *OutlookProvider) UpdateEvent(ctx context.Context, calendarID string, event *Event) error {
	gEvent := eventToOutlookEvent(event)
	body, err := json.Marshal(gEvent)
	if err != nil {
		return err
	}

	reqURL := fmt.Sprintf("%s/me/events/%s", graphBaseURL, event.ID)
	req, err := http.NewRequestWithContext(ctx, "PATCH", reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("updating outlook event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update event failed %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (o *OutlookProvider) DeleteEvent(ctx context.Context, calendarID string, eventID string) error {
	reqURL := fmt.Sprintf("%s/me/events/%s", graphBaseURL, eventID)
	req, err := http.NewRequestWithContext(ctx, "DELETE", reqURL, nil)
	if err != nil {
		return err
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("deleting outlook event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete event failed %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// Graph API types

type graphEventsResponse struct {
	Value     []graphEvent `json:"value"`
	NextLink  string       `json:"@odata.nextLink"`
	DeltaLink string       `json:"@odata.deltaLink"`
}

type graphEvent struct {
	ID          string            `json:"id"`
	Subject     string            `json:"subject"`
	Start       graphDateTime     `json:"start"`
	End         graphDateTime     `json:"end"`
	Location    *graphLocation    `json:"location,omitempty"`
	BodyPreview string            `json:"bodyPreview"`
	ShowAs      string            `json:"showAs"`
	IsAllDay    bool              `json:"isAllDay"`
	ExtProps    []graphExtProp    `json:"singleValueExtendedProperties,omitempty"`
}

type graphDateTime struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

type graphLocation struct {
	DisplayName string `json:"displayName"`
}

type graphExtProp struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

func outlookEventToEvent(item *graphEvent, calendarID string) *Event {
	ev := &Event{
		ID:          item.ID,
		CalendarID:  calendarID,
		Title:       item.Subject,
		Description: item.BodyPreview,
		IsAllDay:    item.IsAllDay,
		IsBusy:      item.ShowAs == "busy" || item.ShowAs == "oof" || item.ShowAs == "tentative",
	}

	if item.Location != nil {
		ev.Location = item.Location.DisplayName
	}

	// Parse times — Graph returns UTC times with "UTC" timezone
	if t, err := time.Parse("2006-01-02T15:04:05.0000000", item.Start.DateTime); err == nil {
		ev.Start = t.UTC()
	} else if t, err := time.Parse("2006-01-02T15:04:05", item.Start.DateTime); err == nil {
		ev.Start = t.UTC()
	}

	if t, err := time.Parse("2006-01-02T15:04:05.0000000", item.End.DateTime); err == nil {
		ev.End = t.UTC()
	} else if t, err := time.Parse("2006-01-02T15:04:05", item.End.DateTime); err == nil {
		ev.End = t.UTC()
	}

	// Check for source tag
	for _, prop := range item.ExtProps {
		if prop.Value != "" {
			ev.SourceTag = prop.Value
			break
		}
	}

	return ev
}

func eventToOutlookEvent(event *Event) *graphEvent {
	gEvent := &graphEvent{
		Subject:  event.Title,
		IsAllDay: event.IsAllDay,
		Start: graphDateTime{
			DateTime: event.Start.UTC().Format("2006-01-02T15:04:05"),
			TimeZone: "UTC",
		},
		End: graphDateTime{
			DateTime: event.End.UTC().Format("2006-01-02T15:04:05"),
			TimeZone: "UTC",
		},
	}

	if event.Location != "" {
		gEvent.Location = &graphLocation{DisplayName: event.Location}
	}

	if event.IsBusy {
		gEvent.ShowAs = "busy"
	} else {
		gEvent.ShowAs = "free"
	}

	if event.SourceTag != "" {
		gEvent.ExtProps = []graphExtProp{
			{
				ID:    fmt.Sprintf("String {00020329-0000-0000-C000-000000000046} Name %s", SourceTagProperty),
				Value: event.SourceTag,
			},
		}
	}

	return gEvent
}

func init() {
	// Suppress unused import warnings
	_ = log.Println
}
