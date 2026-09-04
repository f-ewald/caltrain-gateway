# Changelog

## Unreleased

- Add a Prometheus exporter at `GET /metrics` (unauthenticated, standard for scraping): per-route
  HTTP request count and duration, 511 proxy cache hit/miss, and upstream 511 call outcome and
  latency, under the `caltrain_gateway` metric namespace. Also exposes the Go runtime/process
  metrics the client library registers by default.

## v1.7.0 (2026-09-03)

- Add calendar overrides: an admin can force a specific schedule type (weekday, saturday,
  sunday, holiday) for a given date, taking precedence over the 511 holiday-calendar-derived
  determination used by `GET /caltrain/scheduletype` (which now reports `overridden`) and by the
  departure tracker's stored schedule metadata. Manage overrides at `/admin/calendar`. Applies
  globally to Caltrain (`CT`) for now; multi-agency selection is deferred.

## v1.6.0 (2026-08-26)

- **Breaking:** remove the root catch-all proxy. The service no longer forwards arbitrary
  unrecognised paths to 511; they return `404` instead. Root-level `/transit/` paths are still
  proxied, so existing API callers are unaffected, and `/proxy/` is unchanged.
- `/admin/` now returns an explanatory `503` when the database is unusable or `DATABASE_URL`
  carries no credentials, instead of falling through to the proxy and returning an upstream `401`
  that looked like a rejected login
- Routes are registered on a dedicated `http.ServeMux` rather than the global default, which makes
  the routing table testable

## v1.5.1 (2026-08-25)

- Show the software version, derived from the git tag, on the `/ui` dashboard and in the admin
  page header. The version is stamped at build time via ldflags; an unstamped build reports
  `dev` plus the embedded commit rather than claiming to be a release.
- Build and CI now pass the version and revision into the Docker image
- Trigger CI on pushes to `master` and on pull requests targeting it. The workflow referenced a
  `main` branch that does not exist, so every build in the repository's history had been
  triggered by a tag and pull requests were never validated.

## v1.5.0 (2026-08-25)

- Add `GET /caltrain/timetable/version` reporting a content-derived schedule version, the
  timetable's validity window, days until it expires, and whether the copy is stale
- Add a weak `ETag` and `If-None-Match` handling to `/caltrain/timetable`, so clients can
  revalidate a cached copy with `304 Not Modified` instead of re-downloading it
- Refresh the timetable nightly at 03:00 Pacific (`TIMETABLE_REFRESH_HOUR`) instead of only at
  startup, so the served schedule and its version track upstream changes
- Make timetable loading atomic: a run that cannot load every line leaves the previous schedule
  in place rather than publishing one with lines missing
- Pause departure polling outside service hours, derived from the timetable itself, and close out
  the day before pausing so the last trains' rows are finalized
- Fix gzip framing being appended to bodyless responses such as `304 Not Modified`, which also
  wrongly advertised `Content-Encoding: gzip`

## v1.4.0 (2026-08-25)

- Fix departure polling aborting on the live 511 feed, which sends `""` for boolean fields.
  Because the feed is fetched as one agency-wide document, this discarded every train in the
  response, so no departures were recorded at all. Scalar decoding is now total.
- Report a failed database connection clearly at startup. A bad `DATABASE_URL` previously
  produced only a warning followed by "no database configured", which read as though none
  had been supplied at all.
- Unify the admin panel under a shared tabbed layout; `/admin/` now redirects to the first tab
- Add `PORT` to configure the listen port, defaulting to `8080`. It was previously
  documented but not implemented, so setting it had no effect.
- Track observed train departure times in the `train_departures` table by polling the 511 SIRI
  StopMonitoring feed, recording scheduled vs. observed times, delays, station, direction, line,
  operating day and day of week for delay analysis
- Add paginated admin pages and a JSONL export for recorded departures under `/admin/departures`
- Add `DEPARTURE_TRACKING_ENABLED` and `DEPARTURE_POLL_INTERVAL` configuration
- Embed the timezone database in the binary so operating-day calculations are correct on the
  tzdata-less Alpine runtime image

## v1.3.4 (2026-05-04)

- Harmonize the header design across the support and service alert list pages

## v1.3.3 (2026-04-28)

- Avoid consuming the ID sequence when a service alert refresh changes nothing; an unchanged
  alert now updates `last_seen_at` instead of advancing the `SERIAL` sequence

## v1.3.2 (2026-04-27)

- Fix the service alerts JSON field mapping so entities and nested translations decode correctly
- Update the example fixture and tests to match the real API response

## v1.3.1 (2026-04-27)

- Initialize the database before the first service alerts fetch, so alerts retrieved during
  startup are persisted instead of silently dropped

## v1.3.0 (2026-04-27)

- Persist GTFS-RT service alerts to the `service_alerts` table, deduplicated by entity ID and
  content hash so identical refreshes only update `last_seen_at`
- Add admin pages and a JSONL export for persisted alerts
- Add an admin landing page linking the available sections

## v1.2.3 (2026-04-07)

- Add admin pages to list, view and delete submitted support requests
- Protect the admin routes with basic auth using the database credentials
- Parse the database username and password out of `DATABASE_URL`

## v1.2.2 (2026-04-07)

- Answer CORS preflight `OPTIONS` requests on `/support` instead of rejecting them

## v1.2.1 (2026-04-07)

- Report 511 API and database connectivity on the statistics dashboard
- Expose API key pool availability through `KeyPool.HasKeys`
- Add `CHANGELOG.md`

## v1.2.0 (2026-04-05)

- Add POST `/support` endpoint for support/feedback submissions
- Add optional PostgreSQL database support for persisting support requests
- Refactor support handler and tests into dedicated files (`support.go`, `support_test.go`)

## v1.1.1 (2026-04-02)

- Update UI, add `/proxy` URL

## v1.1.0 (2026-04-02)

- Add UI for basic statistics
- Refactor HTML

## v1.0.0 (2026-03-30)

- Initial release
- HTTP proxy for the 511.org transit API with response caching and request collapsing via `singleflight`
- Round-robin API key rotation with per-key rate limiting
- Timetable endpoint with weekday/station filtering
- Service alerts endpoint with agency filtering
- Schedule type holiday detection
- Stop ID to parent station name mapping endpoint
- Authentication middleware (`X-API-Key` header)
- Gzip compression middleware
- GitHub Actions CI/CD workflow
- Docker support
