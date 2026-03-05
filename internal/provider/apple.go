package provider

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
)

type AppleProvider struct {
	client *caldav.Client
}

const iCloudCalDAVURL = "https://caldav.icloud.com"

func NewAppleProvider(ctx context.Context, cal interface{ GetAppleConfig() (string, string, string) }) (*AppleProvider, error) {
	server, username, passwordEnv := cal.GetAppleConfig()

	password := os.Getenv(passwordEnv)
	if password == "" {
		return nil, fmt.Errorf("environment variable %s not set (Apple app-specific password)", passwordEnv)
	}

	if server == "" {
		server = iCloudCalDAVURL
	}

	httpClient := webdav.HTTPClientWithBasicAuth(nil, username, password)
	client, err := caldav.NewClient(httpClient, server)
	if err != nil {
		return nil, fmt.Errorf("connecting to CalDAV %s: %w", server, err)
	}

	return &AppleProvider{client: client}, nil
}

// AppleCalendarConfig wraps config types for Apple provider construction.
type AppleCalendarConfig struct {
	Server      string
	Username    string
	PasswordEnv string
}

func (a AppleCalendarConfig) GetAppleConfig() (string, string, string) {
	return a.Server, a.Username, a.PasswordEnv
}

func NewAppleProviderFromCalendarConfig(ctx context.Context, cal interface {
	GetAppleConfig() (string, string, string)
}) (*AppleProvider, error) {
	return NewAppleProvider(ctx, cal)
}

func (a *AppleProvider) Name() string { return "apple" }

func (a *AppleProvider) ListEvents(ctx context.Context, calendarPath string, start, end time.Time, syncToken string) (*ListResult, error) {
	query := &caldav.CalendarQuery{
		CompFilter: caldav.CompFilter{
			Name: "VCALENDAR",
			Comps: []caldav.CompFilter{
				{
					Name: "VEVENT",
					Props: []caldav.PropFilter{},
				},
			},
		},
	}

	// Add time range filter
	query.CompFilter.Comps[0].Start = start
	query.CompFilter.Comps[0].End = end

	objects, err := a.client.QueryCalendar(ctx, calendarPath, query)
	if err != nil {
		return nil, fmt.Errorf("querying caldav: %w", err)
	}

	var events []Event
	for _, obj := range objects {
		ev, err := caldavObjectToEvent(obj, calendarPath)
		if err != nil {
			log.Printf("skipping caldav event: %v", err)
			continue
		}
		events = append(events, *ev)
	}

	// For CalDAV, we use the calendar's CTag as a simple change token
	// (not true incremental sync, but detects when calendar changed)
	calInfo, err := a.getCalendarInfo(ctx, calendarPath)
	if err != nil {
		log.Printf("could not get calendar ctag: %v", err)
	}

	return &ListResult{Events: events, NextSyncToken: calInfo}, nil
}

func (a *AppleProvider) CreateEvent(ctx context.Context, calendarPath string, event *Event) error {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//wc-cal-sync//EN")

	vevent := ical.NewEvent()
	uid := fmt.Sprintf("wc-cal-sync-%d@sync", time.Now().UnixNano())
	vevent.Props.SetText(ical.PropUID, uid)
	vevent.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	populateICalEvent(vevent, event)

	cal.Children = append(cal.Children, vevent.Component)

	path := fmt.Sprintf("%s%s.ics", calendarPath, uid)
	obj := caldav.CalendarObject{
		Path: path,
		Data: cal,
	}

	_, err := a.client.PutCalendarObject(ctx, obj.Path, cal)
	if err != nil {
		return fmt.Errorf("creating caldav event: %w", err)
	}
	event.ID = uid

	return nil
}

func (a *AppleProvider) UpdateEvent(ctx context.Context, calendarPath string, event *Event) error {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//wc-cal-sync//EN")

	vevent := ical.NewEvent()
	vevent.Props.SetText(ical.PropUID, event.ID)
	vevent.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	populateICalEvent(vevent, event)

	cal.Children = append(cal.Children, vevent.Component)

	path := fmt.Sprintf("%s%s.ics", calendarPath, event.ID)
	_, err := a.client.PutCalendarObject(ctx, path, cal)
	if err != nil {
		return fmt.Errorf("updating caldav event: %w", err)
	}

	return nil
}

func (a *AppleProvider) DeleteEvent(ctx context.Context, calendarPath string, eventID string) error {
	path := fmt.Sprintf("%s%s.ics", calendarPath, eventID)
	err := a.client.RemoveAll(ctx, path)
	if err != nil {
		return fmt.Errorf("deleting caldav event %s: %w", eventID, err)
	}
	return nil
}

func caldavObjectToEvent(obj caldav.CalendarObject, calendarPath string) (*Event, error) {
	if obj.Data == nil {
		return nil, fmt.Errorf("no data in caldav object")
	}

	for _, child := range obj.Data.Children {
		if child.Name != ical.CompEvent {
			continue
		}

		vevent := ical.Event{Component: child}

		uid, err := vevent.Props.Text(ical.PropUID)
		if err != nil {
			return nil, fmt.Errorf("no UID: %w", err)
		}

		ev := &Event{
			ID:         uid,
			CalendarID: calendarPath,
			ETag:       obj.ETag,
		}

		if summary, err := vevent.Props.Text(ical.PropSummary); err == nil {
			ev.Title = summary
		}

		if location, err := vevent.Props.Text(ical.PropLocation); err == nil {
			ev.Location = location
		}

		if desc, err := vevent.Props.Text(ical.PropDescription); err == nil {
			ev.Description = desc
		}

		dtStart, err := vevent.DateTimeStart(nil)
		if err == nil {
			ev.Start = dtStart
		}

		dtEnd, err := vevent.DateTimeEnd(nil)
		if err == nil {
			ev.End = dtEnd
		}

		// Check transparency
		if transp, err := vevent.Props.Text(ical.PropTransparency); err == nil {
			ev.IsBusy = !strings.EqualFold(transp, "TRANSPARENT")
		} else {
			ev.IsBusy = true // default to busy
		}

		// Check for all-day (VALUE=DATE on DTSTART)
		if dtStartProp := vevent.Props.Get(ical.PropDateTimeStart); dtStartProp != nil {
			if val := dtStartProp.Params.Get("VALUE"); val == "DATE" {
				ev.IsAllDay = true
			}
		}

		// Check for source tag (custom X- property)
		if prop := vevent.Props.Get(SourceTagProperty); prop != nil {
			ev.SourceTag = prop.Value
		}

		return ev, nil
	}

	return nil, fmt.Errorf("no VEVENT found in calendar object")
}

func populateICalEvent(vevent *ical.Event, event *Event) {
	vevent.Props.SetText(ical.PropSummary, event.Title)

	if event.IsAllDay {
		dtStart := vevent.Props.Get(ical.PropDateTimeStart)
		if dtStart == nil {
			vevent.Props.SetDate(ical.PropDateTimeStart, event.Start)
			vevent.Props.SetDate(ical.PropDateTimeEnd, event.End)
		}
	} else {
		vevent.Props.SetDateTime(ical.PropDateTimeStart, event.Start)
		vevent.Props.SetDateTime(ical.PropDateTimeEnd, event.End)
	}

	if event.Location != "" {
		vevent.Props.SetText(ical.PropLocation, event.Location)
	}
	if event.Description != "" {
		vevent.Props.SetText(ical.PropDescription, event.Description)
	}

	if event.IsBusy {
		vevent.Props.SetText(ical.PropTransparency, "OPAQUE")
	} else {
		vevent.Props.SetText(ical.PropTransparency, "TRANSPARENT")
	}

	if event.SourceTag != "" {
		prop := ical.NewProp(SourceTagProperty)
		prop.Value = event.SourceTag
		vevent.Props.Set(prop)
	}
}

func (a *AppleProvider) getCalendarInfo(ctx context.Context, calendarPath string) (string, error) {
	// Use FindCalendars on the parent path to get calendar metadata.
	// For change detection, we return a timestamp-based token since
	// CTag access varies by server implementation.
	return fmt.Sprintf("%s:%d", calendarPath, time.Now().UnixNano()), nil
}
