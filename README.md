# wc-cal-sync

Self-hosted multi-calendar sync service that replaces Reclaim.ai. Syncs busy blockers across Google Calendar, Microsoft Outlook, and Apple iCloud calendars, and maintains a unified merged view.

## What it does

1. **Busy blockers**: When you have an event on Calendar A, it creates "Busy" blocker events on all your other calendars (B, C, D...) so nobody double-books you.

2. **Merged calendar**: All your real events (with full details) are mirrored to a single Apple Calendar for a unified view.

3. **Privacy**: Other people on each calendar only see "Busy" — your real event details stay private across calendar boundaries.

## Supported providers

| Provider | Auth method | Incremental sync |
|----------|------------|------------------|
| Google Calendar | OAuth2 | Yes (syncToken) |
| Microsoft Outlook | OAuth2 (Azure AD) | Yes (delta queries) |
| Apple iCloud | App-specific password | Poll-based (CalDAV) |

## Setup

### 1. Create OAuth apps

**Google Calendar:**
- Go to [Google Cloud Console](https://console.cloud.google.com/)
- Create a project, enable Calendar API
- Create OAuth2 credentials (Desktop app type)
- Note the Client ID and Client Secret

**Microsoft Outlook:**
- Go to [Azure Portal → App registrations](https://portal.azure.com/#blade/Microsoft_AAD_RegisteredApps/)
- Register an app, add `Calendars.ReadWrite` and `offline_access` permissions
- Create a client secret
- Note Tenant ID, Client ID, Client Secret

**Apple iCloud:**
- Go to [appleid.apple.com](https://appleid.apple.com/) → Sign-In & Security → App-Specific Passwords
- Generate an app-specific password

### 2. Configure

```bash
cp config.example.yaml config.yaml
# Edit config.yaml with your calendar IDs and credentials
```

### 3. Run

**One-time sync:**
```bash
go run ./cmd/server -config config.yaml -once
```

**HTTP server (for Cloud Run / continuous sync):**
```bash
go run ./cmd/server -config config.yaml
```

**Docker:**
```bash
docker build -t wc-cal-sync .
docker run -p 8080:8080 \
  -v $(pwd)/config.yaml:/config/config.yaml \
  -v $(pwd)/tokens:/tokens \
  -v $(pwd)/data:/data \
  -e APPLE_APP_PASSWORD=your-password \
  wc-cal-sync
```

### 4. Deploy to Cloud Run

```bash
gcloud run deploy wc-cal-sync \
  --source . \
  --region us-central1 \
  --allow-unauthenticated=false

# Set up Cloud Scheduler to POST to /sync every 5 minutes
gcloud scheduler jobs create http wc-cal-sync-trigger \
  --schedule="*/5 * * * *" \
  --uri="YOUR_CLOUD_RUN_URL/sync" \
  --http-method=POST
```

## API

- `GET /health` — Health check
- `POST /sync` — Trigger a sync cycle
- `GET /sync` — Trigger a sync cycle (convenience)

## How blockers work

Each blocker event carries a custom property (`X-WC-SYNC-SOURCE`) linking it to the original event. This allows the sync engine to:
- Update blockers when source events change time
- Delete blockers when source events are cancelled
- Avoid creating duplicate blockers

## Architecture

```
cmd/server/main.go     → HTTP server + periodic sync
internal/config/       → YAML config loading
internal/provider/     → Calendar provider interface + Google/Outlook/Apple implementations
internal/sync/engine   → Sync orchestration (read → create blockers → merge → cleanup)
internal/sync/merger   → Merged calendar writer
internal/store/        → Persistent state (sync tokens, blocker mappings)
internal/oauth/        → OAuth2 token management + interactive flow
```
