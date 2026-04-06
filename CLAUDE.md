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

# Docker build
docker build -t caltrain-gateway .
```

## Environment Variables

Copy `.env.example` to `.env`. Required variables:
- `FIVEONEONE_API_KEY_1` (and optionally `_2`, `_3`, etc.) — 511.org API keys
- `CALTRAIN_GATEWAY_SECRET` — Secret for `X-API-Key` header authentication (optional, skipped if empty)
- `DATABASE_URL` — PostgreSQL connection string (optional, e.g. `postgres://user:pass@localhost:5432/caltrain?sslmode=disable`). If empty, the app runs without a database.

## Architecture

Go HTTP service that proxies and caches requests to the 511.org transit API for Caltrain data. Listens on port 8080.

### Package Structure

- `cmd/caltrain-gateway/main.go` — Entry point. Loads API keys, fetches timetables for all lines at startup, configures routes.
- `internal/app/caltrain-gateway/` — All application logic in a single package (`caltraingateway`):
  - **http.go** — HTTP handlers, middleware stack (logging, auth, gzip), API proxy with response caching and request collapsing via `singleflight`
  - **timetable.go** — Timetable data model (deep struct hierarchy mirroring 511 API JSON), parsing, and departure queries by stop/weekday. `TimetableCollection` aggregates multiple line timetables.
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

### CI/CD

GitHub Actions workflow builds and pushes Docker image to Azure Container Registry on pushes to `main`.
