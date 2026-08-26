# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
# Build
go build -o caltrain-gateway ./cmd/caltrain-gateway

# Run (requires .env with API keys)
./caltrain-gateway

# Run tests
go test ./...

# Run a single test
go test ./internal/app/caltrain-gateway/ -run TestFunctionName

# Run the database integration tests (skipped by default).
# Point at a scratch database; the departure tests truncate train_departures.
CALTRAIN_TEST_DATABASE_URL="postgres://localhost:5432/caltrain_test?sslmode=disable" \
  go test ./internal/app/caltrain-gateway/

# Docker build
docker build -t caltrain-gateway .
```

## Environment Variables

Copy `.env.example` to `.env`. Required variables:
- `FIVEONEONE_API_KEY_1` (and optionally `_2`, `_3`, etc.) — 511.org API keys
- `CALTRAIN_GATEWAY_SECRET` — Secret for `X-API-Key` header authentication (optional, skipped if empty)
- `DATABASE_URL` — PostgreSQL connection string (optional, e.g. `postgres://user:pass@localhost:5432/caltrain?sslmode=disable`). If empty, the app runs without a database.
- `DEPARTURE_TRACKING_ENABLED` — Record observed departure times (optional, default `true`). Automatically skipped when no database is configured.
- `DEPARTURE_POLL_INTERVAL` — Real-time poll interval as a Go duration (optional, default `2m`, clamped to a `1m` floor).

## Architecture

Go HTTP service that proxies and caches requests to the 511.org transit API for Caltrain data. Listens on the port given by `PORT`, defaulting to 8080.

### Package Structure

- `cmd/caltrain-gateway/main.go` — Entry point. Loads API keys, fetches timetables for all lines at startup, configures routes.
- `internal/app/caltrain-gateway/` — All application logic in a single package (`caltraingateway`):
  - **http.go** — HTTP handlers, middleware stack (logging, auth, gzip), API proxy with response caching and request collapsing via `singleflight`
  - **timetable.go** — Timetable data model (deep struct hierarchy mirroring 511 API JSON), parsing, and departure queries by stop/weekday. `TimetableCollection` aggregates multiple line timetables.
  - **stopmonitoring.go** — SIRI StopMonitoring model and parsing for the real-time feed. Includes tolerant `flexString`/`flexBool` decoders because SIRI producers vary in how they encode scalars.
  - **departures.go** — `DepartureTracker`: polls StopMonitoring, derives operating day / day of week / delays, and finalizes rows whose stop visit has left the feed
  - **departures_store.go** — `train_departures` row model and queries (converging upsert, paginated list, export stream, finalize sweep)
  - **departures_http.go** — Admin handlers, view model and formatting for `/admin/departures`
  - **admin.go** — Shared admin chrome: the tab definitions, the `adminPage` wrapper and `renderAdminPage`, which renders a page's `content` block inside `web/admin_layout.html`. Every admin page goes through it, so layout and styling live in one place.
  - **version.go** — Software version reported by the UI and admin pages, stamped via ldflags with a VCS fallback. Distinct from the *schedule* version in `schedule_version.go`, which identifies timetable content.
  - **schedule_version.go** — Content-derived schedule version and validity metadata, plus ETag construction
  - **schedule_state.go** — Mutex-guarded holder for the timetable, its version and metadata, swapped atomically
  - **schedule_refresh.go** — Nightly timetable refresh scheduled against the wall clock
  - **schedule_http.go** — `/caltrain/timetable/version` handler
  - **service_window.go** — Derives service hours from the timetable to gate departure polling
  - **lines.go** — Transit line model and loading (from file or URL)
  - **ratelimiter.go** — `KeyPool` for round-robin API key rotation with per-key rate limiting (`golang.org/x/time/rate`)
  - **stops.go** — Static mapping of GTFS stop IDs to parent station names (e.g., `"70011"` → `"san_francisco"`)
  - **cache.go** — Global response cache (2-min TTL, `go-cache`)
  - **environment.go** — Environment variable loading

### API Endpoints

- `GET /up` — Health check
- `GET /caltrain/timetable` — All departures by stop ID
- `GET /caltrain/timetable?weekday=Monday&station=san_francisco` — Filter by weekday and/or station name

### Request Flow

1. Requests go through middleware chain: auth/logging (`X-API-Key`) → gzip
2. The proxy handler checks cache → uses `singleflight` for request collapsing → picks an API key from the pool → forwards to 511.org → caches 200 responses
3. `/caltrain/timetable` serves pre-loaded timetable data (loaded at startup), filtered by optional `weekday` and `station` query params

### Routing

`SetupRoutes` builds and returns its own `*http.ServeMux`, which `main` serves. It deliberately
does not register `"/"`: in `net/http` that is the catch-all, and the proxy used to live there, so
the service forwarded every unrecognised path upstream. Unmatched paths now return 404 and no
longer consume the 511 quota.

- The proxy is served at `/proxy/` (prefix stripped) and at `/transit/` (prefix **not** stripped).
  Leaving `/transit/` intact is what makes both forms produce the same upstream URL and therefore
  the same cache key, so do not add a `StripPrefix` there.
- When `DATABASE_URL` has no credentials the admin routes cannot authenticate, so `/admin/` serves
  an explanatory 503 instead. Previously that path fell through to the catch-all and was proxied,
  returning an upstream 401 that looked like a rejected login.

### Departure Tracking

`DepartureTracker` polls the agency-wide SIRI StopMonitoring feed (default every 2 minutes) and
converges one `train_departures` row per `(service_date, train_number, stop_id)`.

Key constraints to keep in mind when changing this code:

- **Departures are inferred.** 511 leaves `ActualDepartureTime` null, so the stored departure is
  the last `ExpectedDepartureTime` seen before the stop visit left the feed. Do not rename columns
  or docs in a way that implies the value is directly reported.
- **Finalization must be outage-guarded.** `Finalize` skips the sweep when the last successful poll
  is older than the grace window; otherwise an API outage would finalize every live row at a stale
  prediction.
- **Dates use the operating day** (3am Pacific boundary) so post-midnight trains stay on the day
  their run began, and `day_of_week` derives from that date, never from the UTC timestamp.
- **`main.go` must keep the `_ "time/tzdata"` import.** The runtime image is a bare Alpine with no
  tzdata, so without it every timezone lookup silently falls back to UTC in production.
- **API quota is 60 requests/hour per key across all endpoints.** Keep the poll interval at or
  above the 1-minute floor.

### Schedule Versioning and Refresh

The timetable is refreshed nightly at 03:00 Pacific and exposed to clients through a version
endpoint and an ETag.

Invariants to preserve when changing this code:

- **The version hash requires canonical ordering.** `GetDeparturesByStop` appends in whatever
  order the upstream lines endpoint returned. `ScheduleVersion` sorts before hashing; removing
  that sort would make the version flip on an upstream reshuffle and stampede every client into
  re-downloading.
- **Refresh is all-or-nothing.** `loadAllTimetables` fails rather than returning a partial
  collection, and `scheduleState.Publish` ignores nil. A partial refresh would drop whole lines
  and change the version.
- **Scheduling is wall-clock, not interval.** `nextRunAt` computes the next 03:00 local. A 24h
  ticker would drift across DST into service hours. 03:00 is also DST-safe; 02:00 does not exist
  on the spring-forward day.
- **Validity dates use only their calendar date.** 511 emits a fixed `-08:00` offset year round,
  so honouring it would push a summer end-of-day bound onto the next day.
- **Departure polling pauses outside service hours**, derived from the timetable, and fails open
  when no timetable is loaded. It is not a bug that the poller is idle overnight.
- **Bodyless responses must not be gzipped.** `gzipResponseWriter` tracks the status so a 304
  neither carries gzip framing nor advertises `Content-Encoding`.

### CI/CD

GitHub Actions workflow builds and pushes a Docker image to Azure Container Registry on pushes to `master`, on `v*` tags, and for pull requests targeting `master` (build only, no push). Note that `latest` tracks the default branch, so it follows `master` rather than the most recent release tag.
