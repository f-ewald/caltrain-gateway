# Caltrain Gateway

A gateway service for accessing Caltrain schedule and real-time transit information.

## Overview

Caltrain Gateway provides an interface to retrieve Caltrain schedules, arrivals, and service updates.

## Prerequisites

You will need to obtain a 511 API key to use this service. Sign up at [511.org](https://511.org/developer-services).

## Installation

1. Clone the repository:
    ```bash
    git clone https://github.com/fewald/caltrain-gateway.git
    cd caltrain-gateway
    ```

2. Configure environment variables:
    ```bash
    cp .env.example .env
    # Edit .env with your configuration
    ```

## Build
```bash
go build -o caltrain-gateway ./cmd/caltrain-gateway
```

To stamp the version reported by `/ui` and the admin pages, pass it at link time:

```bash
PKG=caltrain-gateway/internal/app/caltrain-gateway
go build -ldflags "-X $PKG.buildVersion=$(git describe --tags)" -o caltrain-gateway ./cmd/caltrain-gateway
```

Without it the binary reports `dev` plus the commit the Go toolchain embeds, so a
development build never claims to be a release. Docker and CI builds stamp this
automatically from the git tag.

## Usage

```bash
./caltrain-gateway
```

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | TCP port the server listens on | `8080` |
| `FIVEONEONE_API_KEY_1` | 511.org API key (add `_2`, `_3`, … for more) | — |
| `CALTRAIN_GATEWAY_SECRET` | Secret for `X-API-Key` authentication | — |
| `DATABASE_URL` | PostgreSQL connection string; omit to run without a database | — |
| `DEPARTURE_TRACKING_ENABLED` | Record observed departure times | `true` |
| `DEPARTURE_POLL_INTERVAL` | How often to poll the real-time feed (minimum `1m`) | `2m` |
| `TIMETABLE_REFRESH_HOUR` | Local hour of the nightly timetable refresh (0–23) | `3` |

`CALTRAIN_TEST_DATABASE_URL` is read only by the test suite; see the database
integration tests, which skip unless it is set.

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/up` | Health check |
| GET | `/proxy/*` | Passthrough proxy to the 511 API, with caching and key rotation |
| GET | `/transit/*` | The same proxy at the root, kept for backwards compatibility |
| GET | `/caltrain/timetable` | Get all train departures by stop ID |
| GET | `/caltrain/timetable?weekday=Monday` | Get departures filtered by weekday |
| GET | `/caltrain/timetable/version` | Schedule version, validity window and freshness |
| GET | `/caltrain/stops` | GTFS stop ID → station name mapping |
| GET | `/caltrain/servicealerts` | Service alerts, optionally `?agency=` filtered |
| GET | `/caltrain/scheduletype?date=` | Which schedule (weekday/saturday/sunday/holiday) applies to a date |

### Generic, agency-aware endpoints

Each `/caltrain/*` endpoint above also has a generic equivalent under `/agency/{operator}/...`,
e.g. `/agency/CT/timetable`, `/agency/BA/timetable`, `/agency/CT/scheduletype?date=2026-09-07`.
Both `/caltrain/*` and `/agency/CT/...` are served by the same code and return identical
responses — the `/caltrain/*` paths are not deprecated and keep working alongside the generic ones.

Two agencies have real, locally-loaded `timetable`/`timetable/version`/`stops` data: Caltrain
(`CT`) and BART (`BA`, monitored lines only — see [Schedule freshness](#schedule-freshness)).
Requesting any other agency through those three returns `404` rather than silently mislabelling
Caltrain's schedule as another agency's. `scheduletype` and `servicealerts` already accept any
agency ID transparently — the former talks directly to 511's holiday-calendar API, and the latter's
alerts are fetched for both CT and BA and merged, with any other agency simply returning an empty
result.

The proxy is served under `/proxy/`. Root-level `/transit/` paths remain supported for existing
callers and forward to the same upstream URL, so both forms share cached responses. No catch-all
is registered: any other unrecognised path returns `404` rather than being forwarded to 511.

Admin pages (`/admin/…`) require `DATABASE_URL` to include credentials, since they authenticate
with the database username and password over HTTP basic auth. When that is missing, `/admin/`
returns `503` explaining which of the two causes applies and how to fix it.

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/admin/departures` | Browse recorded departures (paginated, filterable) |
| GET | `/admin/departures/detail?id=` | Inspect a single recorded departure |
| POST | `/admin/departures/delete?id=` | Delete a recorded departure |
| GET | `/admin/departures/export` | Download matching departures as JSONL |

## Timetable

The timetable module parses Caltrain schedule data and provides departures grouped by stop ID. Each departure includes train ID, line, direction, arrival/departure times, and destination. Schedules are filtered by day type (weekday/weekend) based on the `weekday` query parameter.

Supported weekday values: `Monday`, `Tuesday`, `Wednesday`, `Thursday`, `Friday`, `Saturday`, `Sunday`

## Schedule freshness

The gateway serves a copy of Caltrain's timetable, refreshed nightly at 03:00 Pacific
(`TIMETABLE_REFRESH_HOUR`). Overnight keeps the ~6 upstream requests clear of daytime traffic,
which already carries departure polling against a 60 requests/hour per-key budget.

It also serves BART's timetable (monitored lines only — 10 of BART's 14, excluding bus bridges and
other non-real-time-tracked variants), refreshed at most once a week rather than nightly: BART has
roughly 3x Caltrain's lines and a correspondingly larger payload, and its schedule changes less
often, so a full weekly re-fetch is enough. Both agencies' refreshers wake daily at the same hour,
but BART's skips the actual fetch until a week has passed since its last successful one.

Refreshes are **atomic** per agency: a run either loads every line for that agency or is abandoned,
leaving that agency's previous schedule in place. A partial refresh would drop whole lines and
change the version, pushing every client to download a truncated timetable.

Clients can validate a cached copy two ways.

**`GET /caltrain/timetable/version`** — one small response regardless of how many timetable
variants a client caches:

```json
{
  "version": "3f9c1a4e2b7d8051",
  "valid_from": "2026-01-31",
  "valid_to": "2026-08-31",
  "expires_in_days": 6,
  "expired": false,
  "frame_ids": ["Timetable:3206643", "Timetable:3206644"],
  "line_count": 5,
  "refreshed_at": "2026-08-25T10:00:00Z",
  "stale": false
}
```

- `version` is a digest of the schedule content, so it changes exactly when the departures a
  client consumes change. It is derived from a canonically ordered projection, so an upstream
  reordering of lines does not produce a spurious change.
- `expires_in_days` and `expired` come from the timetable's own validity window, letting a client
  prefetch ahead of a cutover rather than discovering it afterwards.
- `stale` means no refresh has succeeded recently, distinguishing "verified current" from merely
  "not rechecked". Note that the validity dates carry a fixed `-08:00` offset upstream regardless
  of daylight saving, so only their calendar date is used.

**`ETag` / `If-None-Match`** on `/caltrain/timetable` — a weak ETag is returned with every
timetable response, and a matching `If-None-Match` yields `304 Not Modified` with no body. The
tag covers the `weekday` and `station` parameters as well as the schedule version, since the
response body varies by those.

```bash
curl -sI localhost:8080/caltrain/timetable | grep -i etag
curl -s -o /dev/null -w '%{http_code}\n' \
  -H 'If-None-Match: W/"3f9c1a4e2b7d8051-all-all"' \
  localhost:8080/caltrain/timetable
```

## Departure tracking

When a database is configured, the gateway polls the 511 SIRI StopMonitoring feed and records one
row per train, stop and operating day in `train_departures`, building a dataset for analysing when
trains are typically delayed.

**Departure times are inferred, not reported.** 511 does not populate SIRI's `ActualDepartureTime`,
so it only publishes the scheduled time (`AimedDepartureTime`) and a live prediction
(`ExpectedDepartureTime`). A train's stop visit disappears from the feed once it has departed, so
the last prediction observed before the visit vanishes is stored as the departure. A row is stamped
`finalized_at` once its visit has been absent for three poll intervals; until then the value may
still change.

Each row keeps the evidence behind its estimate, which matters when using the data for modelling:

- `observation_count` and `last_seen_at` bound the accuracy — at the default 2-minute cadence the
  final observation can precede the real departure by up to two minutes.
- `monitored` is false when the train had no AVL signal. The feed then echoes the scheduled time
  back as the "prediction", so these rows look perfectly on time and **should be excluded from
  delay analysis** rather than treated as punctual.
- `vehicle_at_stop` records whether the train was ever actually observed at the stop, which
  distinguishes a genuine departure from a visit that vanished for another reason, such as a
  cancellation.
- `departure_source` is `expected` for the normal inferred case, or `actual` in the event that 511
  ever does supply an authoritative time.

Dates use the **operating day** (starting at 3am Pacific), so late-night trains that run past
midnight stay grouped with the day their run began, and `day_of_week` is derived from that
operating day rather than from the UTC timestamp. `schedule_type` reuses the holiday calendar to
mark each day as weekday, saturday, sunday or holiday; it is left empty when the calendar could not
be fetched.

### API quota

511 allows **60 requests per hour per API key**, shared across every endpoint. The default
2-minute interval uses 30 of those per hour, leaving headroom for proxy traffic and the service
alert refresh. `DEPARTURE_POLL_INTERVAL` is clamped to a 1-minute floor for that reason. A single
agency-wide request covers every Caltrain stop, so the cost does not scale with the number of
stations.

## Lines

Lines represent the different Caltrain services (Limited, Local, Express, etc.). Each line includes metadata such as validity dates, transport mode, public code, and monitoring status. Lines can be loaded from a local file or fetched from the 511 API.

## License

[MIT License](LICENSE)
