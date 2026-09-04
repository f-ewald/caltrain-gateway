package caltraingateway

import (
	"log"
	"time"
)

// TimetableLoader fetches a complete timetable collection from upstream. It must
// return an error rather than a partial collection when any line fails, so the
// refresh can be all-or-nothing.
type TimetableLoader func(apiKey string) (*TimetableCollection, error)

// refreshRetryDelay is how soon to retry when the nightly run cannot proceed,
// for example because every API key is rate limited. Retrying shortly avoids
// waiting a further day for a transient condition.
const refreshRetryDelay = 15 * time.Minute

// ScheduleRefresher re-fetches one agency's timetable on a schedule.
//
// Refreshing overnight (rather than during the day) keeps the upstream requests
// clear of daytime traffic, which already carries departure polling and
// proxied requests against a 60 requests/hour per-key budget. minInterval lets
// an agency with a larger payload (more lines) refresh less often than nightly
// without a separate scheduling mechanism: the goroutine still wakes at `hour`
// every day, but skips the actual fetch until minInterval has elapsed since the
// last successful one.
type ScheduleRefresher struct {
	pool        *KeyPool
	operatorID  string
	loader      TimetableLoader
	hour        int
	minInterval time.Duration
}

// NewScheduleRefresher creates a refresher for operatorID that runs at the
// given local hour, skipping runs until minInterval has elapsed since the last
// success. Pass 0 for minInterval for a plain daily refresh.
func NewScheduleRefresher(pool *KeyPool, operatorID string, loader TimetableLoader, hour int, minInterval time.Duration) *ScheduleRefresher {
	return &ScheduleRefresher{pool: pool, operatorID: operatorID, loader: loader, hour: hour, minInterval: minInterval}
}

// Start runs the refresh loop in its own goroutine.
func (s *ScheduleRefresher) Start() {
	if s.loader == nil || !s.pool.HasKeys() {
		log.Printf("Timetable refresh disabled for %s: no API keys available", s.operatorID)
		return
	}
	if s.minInterval > 24*time.Hour {
		log.Printf("Timetable refresh scheduled for %s at %02d:00 %s, at most every %s",
			s.operatorID, s.hour, pacificLocation(), s.minInterval)
	} else {
		log.Printf("Timetable refresh scheduled for %s daily at %02d:00 %s", s.operatorID, s.hour, pacificLocation())
	}
	go s.run()
}

// run sleeps until each scheduled time and refreshes.
func (s *ScheduleRefresher) run() {
	// A failed startup load leaves nothing to serve, and the next scheduled
	// slot could be a day or more away, so recover promptly before settling
	// into the regular schedule. This initial catch-up ignores minInterval:
	// there is nothing to protect by waiting when nothing has loaded yet.
	for scheduleFor(s.operatorID).Collection() == nil {
		if s.Refresh() {
			break
		}
		time.Sleep(refreshRetryDelay)
	}

	for {
		wait := time.Until(nextRunAt(time.Now(), s.hour))
		time.Sleep(wait)

		if !s.dueForRefresh() {
			continue
		}
		if !s.Refresh() {
			time.Sleep(refreshRetryDelay)
			s.Refresh()
		}
	}
}

// dueForRefresh reports whether enough time has passed since the last
// successful refresh to justify another fetch. minInterval <= 0 means every
// scheduled wake-up is due, preserving a plain daily refresh.
func (s *ScheduleRefresher) dueForRefresh() bool {
	if s.minInterval <= 0 {
		return true
	}
	_, _, _, refreshedAt, _ := scheduleFor(s.operatorID).Snapshot()
	return refreshedAt.IsZero() || time.Since(refreshedAt) >= s.minInterval
}

// Refresh fetches a fresh timetable and publishes it, reporting whether it
// succeeded.
//
// Publication is atomic: the loader either returns a complete collection or an
// error, and only a complete one replaces the served schedule. A partial
// refresh would drop whole lines from the timetable and change the version,
// pushing every client to download a truncated schedule.
func (s *ScheduleRefresher) Refresh() bool {
	state := scheduleFor(s.operatorID)

	key, ok := s.pool.GetAvailableKey()
	if !ok {
		log.Printf("Warning: no available API key to refresh the %s timetable", s.operatorID)
		state.RecordFailedAttempt()
		return false
	}

	previous := state.Version()
	collection, err := s.loader(key.Value)
	if err != nil {
		log.Printf("Warning: %s timetable refresh failed, keeping the existing schedule: %v", s.operatorID, err)
		state.RecordFailedAttempt()
		return false
	}

	state.Publish(collection)
	current := state.Version()
	if current != previous {
		log.Printf("Timetable refreshed for %s: schedule version %s -> %s", s.operatorID, previous, current)
		return true
	}
	log.Printf("Timetable refreshed for %s: unchanged at version %s", s.operatorID, current)
	return true
}

// nextRunAt returns the next occurrence of the given local hour strictly after
// now.
//
// The schedule is anchored to the wall clock rather than a fixed interval: a
// 24h ticker drifts across daylight saving changes and would eventually run
// during service hours, which is exactly what the overnight slot avoids.
func nextRunAt(now time.Time, hour int) time.Time {
	local := now.In(pacificLocation())
	next := time.Date(local.Year(), local.Month(), local.Day(), hour, 0, 0, 0, local.Location())
	if !next.After(local) {
		next = next.AddDate(0, 0, 1)
		// Re-normalise through time.Date so a day that does not contain this
		// hour, or one where it repeats, resolves against the real calendar.
		next = time.Date(next.Year(), next.Month(), next.Day(), hour, 0, 0, 0, local.Location())
	}
	return next
}
