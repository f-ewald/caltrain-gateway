# Changelog

## Unreleased

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
