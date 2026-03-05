# wc-cal-sync

A command-line tool written in Go that merges multiple iCalendar (`.ics`) sources—HTTP/HTTPS feeds or local files—into a single output `.ics` file, deduplicating events by UID.

## Features

- Fetch calendars from HTTP/HTTPS URLs or local `.ics` files
- Merge events from any number of sources into one calendar
- Deduplicate events by UID (first occurrence wins)
- Configure via a YAML file **or** CLI flags
- Configurable HTTP request timeout

## Installation

```bash
go install github.com/WCMinor/wc-cal-sync/cmd/wc-cal-sync@latest
```

Or build from source:

```bash
git clone https://github.com/WCMinor/wc-cal-sync.git
cd wc-cal-sync
go build -o wc-cal-sync ./cmd/wc-cal-sync
```

## Usage

### Using a config file

```bash
wc-cal-sync sync --config config.yaml
```

`config.yaml` example:

```yaml
sources:
  - name: Work Calendar
    url: https://example.com/work.ics
  - name: Personal Calendar
    url: /home/user/personal.ics
output: /home/user/merged.ics
timeout: 30s
```

### Using CLI flags

```bash
wc-cal-sync sync \
  --source https://example.com/work.ics \
  --source /home/user/personal.ics \
  --output merged.ics \
  --timeout 15s
```

### Help

```bash
wc-cal-sync --help
wc-cal-sync sync --help
```

## Development

```bash
# Run tests
go test ./...

# Build
go build ./cmd/wc-cal-sync
```

## License

MIT
