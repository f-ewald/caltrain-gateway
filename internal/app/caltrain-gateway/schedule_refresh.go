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

// ScheduleRefresher re-fetches the timetable once a night.
//
// Refreshing overnight keeps the ~6 upstream requests clear of daytime traffic,
// which already carries departure polling and proxied requests against a
// 60 requests/hour per-key budget.
type ScheduleRefresher struct {
	pool   *KeyPool
	loader TimetableLoader
	hour   int
}

// NewScheduleRefresher creates a refresher that runs daily at the given local hour.
func NewScheduleRefresher(pool *KeyPool, loader TimetableLoader, hour int) *ScheduleRefresher {
	return &ScheduleRefresher{pool: pool, loader: loader, hour: hour}
}

// Start runs the refresh loop in its own goroutine.
func (s *ScheduleRefresher) Start() {
	if s.loader == nil || !s.pool.HasKeys() {
		log.Println("Timetable refresh disabled: no API keys available")
		return
	}
	log.Printf("Timetable refresh scheduled daily at %02d:00 %s", s.hour, pacificLocation())
	go s.run()
}

// run sleeps until each scheduled time and refreshes.
func (s *ScheduleRefresher) run() {
	// A failed startup load leaves nothing to serve, and the nightly slot could
	// be almost a day away, so recover promptly before settling into the
	// overnight schedule.
	for schedule.Collection() == nil {
		if s.Refresh() {
			break
		}
		time.Sleep(refreshRetryDelay)
	}

	for {
		wait := time.Until(nextRunAt(time.Now(), s.hour))
		time.Sleep(wait)

		if !s.Refresh() {
			time.Sleep(refreshRetryDelay)
			s.Refresh()
		}
	}
}

// Refresh fetches a fresh timetable and publishes it, reporting whether it
// succeeded.
//
// Publication is atomic: the loader either returns a complete collection or an
// error, and only a complete one replaces the served schedule. A partial
// refresh would drop whole lines from the timetable and change the version,
// pushing every client to download a truncated schedule.
func (s *ScheduleRefresher) Refresh() bool {
	key, ok := s.pool.GetAvailableKey()
	if !ok {
		log.Println("Warning: no available API key to refresh the timetable")
		schedule.RecordFailedAttempt()
		return false
	}

	previous := schedule.Version()
	collection, err := s.loader(key.Value)
	if err != nil {
		log.Printf("Warning: timetable refresh failed, keeping the existing schedule: %v", err)
		schedule.RecordFailedAttempt()
		return false
	}

	schedule.Publish(collection)
	current := schedule.Version()
	if current != previous {
		log.Printf("Timetable refreshed: schedule version %s -> %s", previous, current)
		return true
	}
	log.Printf("Timetable refreshed: unchanged at version %s", current)
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
