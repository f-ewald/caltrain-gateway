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

`CALTRAIN_TEST_DATABASE_URL` is read only by the test suite; see the database
integration tests, which skip unless it is set.

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/up` | Health check |
| GET | `/caltrain/timetable` | Get all train departures by stop ID |
| GET | `/caltrain/timetable?weekday=Monday` | Get departures filtered by weekday |

Admin pages (`/admin/…`) are registered only when `DATABASE_URL` is set and are protected by
HTTP basic auth using the database credentials.

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/admin/departures` | Browse recorded departures (paginated, filterable) |
| GET | `/admin/departures/detail?id=` | Inspect a single recorded departure |
| POST | `/admin/departures/delete?id=` | Delete a recorded departure |
| GET | `/admin/departures/export` | Download matching departures as JSONL |

## Timetable

The timetable module parses Caltrain schedule data and provides departures grouped by stop ID. Each departure includes train ID, line, direction, arrival/departure times, and destination. Schedules are filtered by day type (weekday/weekend) based on the `weekday` query parameter.

Supported weekday values: `Monday`, `Tuesday`, `Wednesday`, `Thursday`, `Friday`, `Saturday`, `Sunday`

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
