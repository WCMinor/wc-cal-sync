# wc-cal-sync — Architecture Plan

## Overview

Self-hosted multi-calendar sync service in Go that:
1. **Syncs busy blockers** across all calendars (replacing Reclaim.ai)
2. **Merges all real events** into a single Apple Calendar (iCal) for a clean unified view

## Calendars & Providers

| Provider | Protocol | Go Library |
|----------|----------|------------|
| Google Calendar | REST API v3 | `google.golang.org/api/calendar/v3` |
| Microsoft Outlook | Microsoft Graph API | `github.com/microsoftgraph/msgraph-sdk-go` or raw HTTP |
| Apple iCloud | CalDAV | `github.com/emersion/go-webdav` |

ICS event manipulation: `github.com/arran4/golang-ical`

## How It Works

### Sync Flow (runs every N minutes via Cloud Scheduler)

```
1. READ all events from all source calendars (Google, Outlook, Apple)
2. For each real event on calendar A:
   a. Check if blocker already exists on calendars B, C, D...
   b. If not → create "busy" blocker (title="Busy", opaque, no details)
   c. If event was deleted/moved → update or remove stale blockers
3. MERGE all real events → write to designated Apple Calendar (merged view)
   - Real event details: title, time, location, description
   - Skip blocker events (detect by tag/extended property)
4. Clean up orphaned blockers (source event no longer exists)
```

### Blocker Identification

Each blocker event gets a custom property/tag:
- `X-WC-SYNC-SOURCE: {provider}:{calendarId}:{eventId}`
- This lets us track which source event a blocker belongs to
- On Google: use `extendedProperties.private`
- On Outlook: use `singleValueExtendedProperties`
- On Apple/CalDAV: use custom iCal `X-` property

### Privacy Rules

From any external viewer's perspective:
- Calendar A shows: real events on A + "Busy" blocks for all other calendars
- Merged Apple Calendar (private): shows all real events with full details

### Change Detection

- **Google**: `syncToken` on `Events.list()` — returns only changed events since last sync
- **Outlook**: `delta` queries on `/me/calendarView/delta` — same concept
- **Apple/CalDAV**: `ctag` on calendar + `etag` on events — poll and compare

## Project Structure

```
wc-cal-sync/
├── cmd/
│   └── server/
│       └── main.go              # HTTP server entry point (Cloud Run)
├── internal/
│   ├── config/
│   │   └── config.go            # YAML config loading
│   ├── provider/
│   │   ├── provider.go          # Calendar provider interface
│   │   ├── google.go            # Google Calendar implementation
│   │   ├── outlook.go           # Microsoft Graph implementation
│   │   └── apple.go             # CalDAV/iCloud implementation
│   ├── sync/
│   │   ├── engine.go            # Main sync orchestration
│   │   ├── blocker.go           # Blocker creation/cleanup logic
│   │   └── merger.go            # Merged calendar writer
│   └── store/
│       └── state.go             # Sync state persistence (sync tokens, etags)
├── config.yaml                  # User configuration
├── Dockerfile                   # For Cloud Run deployment
├── go.mod
├── go.sum
└── README.md
```

## Configuration (config.yaml)

```yaml
calendars:
  - id: "personal"
    provider: "google"
    calendar_id: "primary"
    credentials_file: "/secrets/google-personal.json"

  - id: "mycompany1"
    provider: "outlook"
    calendar_id: "AAMkAD..."
    tenant_id: "..."
    client_id: "..."
    client_secret_env: "OUTLOOK_COMPANY1_SECRET"

  - id: "mycompany2"
    provider: "outlook"
    calendar_id: "AAMkAD..."
    tenant_id: "..."
    client_id: "..."
    client_secret_env: "OUTLOOK_COMPANY2_SECRET"

  - id: "client1"
    provider: "google"
    calendar_id: "abc@group.calendar.google.com"
    credentials_file: "/secrets/google-client1.json"

  - id: "client2"
    provider: "apple"
    server: "caldav.icloud.com"
    username: "user@icloud.com"
    password_env: "APPLE_APP_PASSWORD"
    calendar_path: "/calendars/client2/"

merged_calendar:
  provider: "apple"
  server: "caldav.icloud.com"
  username: "user@icloud.com"
  password_env: "APPLE_APP_PASSWORD"
  calendar_path: "/calendars/merged-view/"

sync:
  interval_minutes: 5
  lookahead_days: 30
  lookback_days: 7
  blocker_title: "Busy"

state_file: "/data/sync-state.json"
```

## Provider Interface

```go
type Event struct {
    ID          string
    CalendarID  string
    Provider    string
    Title       string
    Start       time.Time
    End         time.Time
    Location    string
    Description string
    IsAllDay    bool
    IsBusy      bool        // transparency: opaque
    SourceTag   string      // X-WC-SYNC-SOURCE value (empty if real event)
    Raw         interface{} // provider-specific data
}

type Provider interface {
    Name() string
    ListEvents(ctx context.Context, calendarID string, start, end time.Time, syncToken string) ([]Event, string, error)
    CreateEvent(ctx context.Context, calendarID string, event Event) error
    UpdateEvent(ctx context.Context, calendarID string, event Event) error
    DeleteEvent(ctx context.Context, calendarID string, eventID string) error
}
```

## Deployment (Cloud Run)

- Docker container with Go binary
- Cloud Scheduler triggers `/sync` endpoint every 5 minutes
- Secrets stored in GCP Secret Manager (mounted as env vars)
- Sync state stored in Cloud Storage or Firestore (simple JSON blob)
- Alternatively: `/sync` can be triggered by Cloud Scheduler as a Cloud Run Job

## Implementation Order

1. **Phase 1**: Provider interface + Google Calendar provider
2. **Phase 2**: Outlook provider (Microsoft Graph)
3. **Phase 3**: Apple/CalDAV provider
4. **Phase 4**: Sync engine (blocker creation/cleanup)
5. **Phase 5**: Merged calendar writer
6. **Phase 6**: HTTP server + Dockerfile + Cloud Run config
7. **Phase 7**: State persistence + incremental sync
