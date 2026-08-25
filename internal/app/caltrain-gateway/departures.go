package caltraingateway

import (
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"
)

const (
	// departureOperatorID is the 511 agency code for Caltrain.
	departureOperatorID = "CT"

	// operatingDayStartHour is the local hour at which a new Caltrain operating
	// day begins. Trains departing before this hour belong to the previous
	// service date, which keeps late-night trains that cross midnight grouped
	// with the day their run started.
	operatingDayStartHour = 3

	// finalizeGraceMultiplier scales the poll interval to decide how long a stop
	// visit must be absent before its row is treated as final.
	finalizeGraceMultiplier = 3
)

// pacificOnce guards the one-time load of the Caltrain service timezone.
var (
	pacificOnce sync.Once
	pacificLoc  *time.Location
)

// pacificLocation returns the America/Los_Angeles location, falling back to UTC
// if the timezone database is unavailable. cmd/caltrain-gateway embeds tzdata so
// the fallback should never trigger in production.
func pacificLocation() *time.Location {
	pacificOnce.Do(func() {
		loc, err := time.LoadLocation("America/Los_Angeles")
		if err != nil {
			log.Printf("Warning: failed to load America/Los_Angeles, falling back to UTC: %v", err)
			loc = time.UTC
		}
		pacificLoc = loc
	})
	return pacificLoc
}

// parseSiriTime parses an RFC3339 timestamp from the SIRI feed. It reports false
// for empty or unparseable input so callers can store SQL NULL instead.
func parseSiriTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

// operatingDate maps an instant to the Caltrain operating day that contains it,
// as a UTC-midnight date. Instants before operatingDayStartHour Pacific belong
// to the previous day.
func operatingDate(t time.Time) time.Time {
	local := t.In(pacificLocation()).Add(-operatingDayStartHour * time.Hour)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

// deriveServiceDate resolves the operating day for a stop visit. It prefers the
// feed's DataFrameRef and otherwise falls back to the operating day of the
// earliest known scheduled time. Reports false when neither is available.
func deriveServiceDate(dataFrameRef string, fallbacks ...time.Time) (time.Time, bool) {
	if dataFrameRef != "" {
		if parsed, err := time.Parse("2006-01-02", dataFrameRef); err == nil {
			return parsed.UTC(), true
		}
	}
	for _, candidate := range fallbacks {
		if !candidate.IsZero() {
			return operatingDate(candidate), true
		}
	}
	return time.Time{}, false
}

// delaySeconds returns the signed difference between an observed and a scheduled
// time in seconds, where negative means early. Returns nil when either side is
// unknown so the column stays NULL rather than defaulting to zero.
func delaySeconds(scheduled, observed time.Time, scheduledOK, observedOK bool) *int {
	if !scheduledOK || !observedOK {
		return nil
	}
	seconds := int(observed.Sub(scheduled).Seconds())
	return &seconds
}

// departureObservation resolves the best available departure time for a stop
// visit. 511 is not expected to populate ActualDepartureTime, but it is
// preferred when present because it is authoritative; otherwise the live
// prediction is used and the row is marked as an inferred value.
func departureObservation(call *MonitoredCall) (time.Time, bool, string) {
	if actual, ok := parseSiriTime(call.ActualDepartureTime); ok {
		return actual, true, "actual"
	}
	if expected, ok := parseSiriTime(call.ExpectedDepartureTime); ok {
		return expected, true, "expected"
	}
	return time.Time{}, false, ""
}

// buildDepartureRow converts a stop visit into a persistable row. It reports
// false when the visit lacks the identifiers required by the unique key, or
// carries no usable timing information at all.
func buildDepartureRow(visit MonitoredStopVisit, scheduleType ScheduleType) (*TrainDepartureRow, bool) {
	journey := &visit.MonitoredVehicleJourney
	call := &journey.MonitoredCall

	trainNumber := journey.TrainNumber()
	stopID := string(call.StopPointRef)
	if stopID == "" {
		stopID = string(visit.MonitoringRef)
	}
	if trainNumber == "" || stopID == "" {
		return nil, false
	}

	aimedArrival, aimedArrivalOK := parseSiriTime(call.AimedArrivalTime)
	expectedArrival, expectedArrivalOK := parseSiriTime(call.ExpectedArrivalTime)
	aimedDeparture, aimedDepartureOK := parseSiriTime(call.AimedDepartureTime)
	observedDeparture, observedDepartureOK, source := departureObservation(call)

	serviceDate, ok := deriveServiceDate(
		string(journey.FramedVehicleJourneyRef.DataFrameRef),
		aimedDeparture, aimedArrival, expectedArrival,
	)
	if !ok {
		return nil, false
	}

	row := &TrainDepartureRow{
		ServiceDate:           serviceDate,
		TrainNumber:           trainNumber,
		StopID:                stopID,
		Station:               GTFSIDToParentName[stopID],
		Direction:             string(journey.DirectionRef),
		Line:                  departureLineName(journey),
		DayOfWeek:             int(serviceDate.Weekday()),
		ScheduleType:          string(scheduleType),
		ArrivalDelaySeconds:   delaySeconds(aimedArrival, expectedArrival, aimedArrivalOK, expectedArrivalOK),
		DepartureDelaySeconds: delaySeconds(aimedDeparture, observedDeparture, aimedDepartureOK, observedDepartureOK),
		DwellSeconds:          delaySeconds(expectedArrival, observedDeparture, expectedArrivalOK, observedDepartureOK),
		DepartureSource:       source,
		VehicleAtStop:         bool(call.VehicleAtStop),
		Monitored:             bool(journey.Monitored),
		VehicleRef:            string(journey.VehicleRef),
	}
	assignOptionalTime(&row.ScheduledArrival, aimedArrival, aimedArrivalOK)
	assignOptionalTime(&row.ExpectedArrival, expectedArrival, expectedArrivalOK)
	assignOptionalTime(&row.ScheduledDeparture, aimedDeparture, aimedDepartureOK)
	assignOptionalTime(&row.ExpectedDeparture, observedDeparture, observedDepartureOK)
	return row, true
}

// assignOptionalTime points target at a copy of value when ok, leaving it nil
// otherwise so the column is written as SQL NULL.
func assignOptionalTime(target **time.Time, value time.Time, ok bool) {
	if !ok {
		return
	}
	copied := value
	*target = &copied
}

// departureLineName returns the human-readable service name for a journey,
// preferring PublishedLineName over the raw LineRef.
func departureLineName(journey *MonitoredVehicleJourney) string {
	if name := string(journey.PublishedLineName); name != "" {
		return name
	}
	return string(journey.LineRef)
}

// DepartureTracker polls the SIRI StopMonitoring feed and converges one row per
// train, stop and operating day. Because 511 never reports an actual departure,
// a row's final expected time — captured just before the stop visit disappears
// from the feed — is the inferred actual departure.
type DepartureTracker struct {
	pool     *KeyPool
	interval time.Duration

	mu                 sync.RWMutex
	lastSuccessfulPoll time.Time
}

// NewDepartureTracker creates a tracker polling at the given interval.
func NewDepartureTracker(pool *KeyPool, interval time.Duration) *DepartureTracker {
	return &DepartureTracker{pool: pool, interval: interval}
}

// Start runs the poll and finalize loops. It returns immediately; both loops run
// in their own goroutines, with an initial poll so data is recorded at startup
// rather than after the first interval elapses. Polling is skipped entirely when
// no database is configured, since there is nowhere to persist to.
func (t *DepartureTracker) Start() {
	if DB == nil {
		log.Println("Departure tracking disabled: no database configured")
		return
	}
	if !t.pool.HasKeys() {
		log.Println("Departure tracking disabled: no API keys available")
		return
	}

	log.Printf("Departure tracking enabled, polling every %s", t.interval)
	go func() {
		t.Poll()
		t.runLoop(t.interval, t.Poll)
	}()
	go t.runLoop(t.finalizeInterval(), t.Finalize)
}

// runLoop invokes fn on a fixed ticker.
func (t *DepartureTracker) runLoop(interval time.Duration, fn func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		fn()
	}
}

// finalizeInterval returns how often stale rows are swept.
func (t *DepartureTracker) finalizeInterval() time.Duration {
	return t.interval * finalizeGraceMultiplier
}

// Poll fetches the agency-wide stop monitoring feed once and upserts every
// visit it contains.
func (t *DepartureTracker) Poll() {
	key, ok := t.pool.GetAvailableKey()
	if !ok {
		log.Println("Warning: no available API key to poll departures")
		return
	}

	response, err := LoadStopMonitoringFromURL(t.buildURL(key.Value))
	if err != nil {
		if err == errRateLimited {
			log.Println("Warning: departure poll rejected, 511 rate limit exceeded")
			return
		}
		log.Printf("Warning: failed to poll departures: %v", err)
		return
	}

	t.record(response)
	t.mu.Lock()
	t.lastSuccessfulPoll = time.Now()
	t.mu.Unlock()
}

// buildURL constructs the agency-wide StopMonitoring request. Omitting
// stopCode returns visits for every Caltrain stop in a single request, which
// keeps the poll within the 60 requests/hour per-key quota.
func (t *DepartureTracker) buildURL(apiKey string) string {
	return fmt.Sprintf("%stransit/StopMonitoring?agency=%s&format=json&api_key=%s",
		apiBaseURL, departureOperatorID, url.QueryEscape(apiKey))
}

// record upserts every visit in the response, logging aggregate counts. Failures
// on individual visits are skipped so one bad record cannot abort the batch.
func (t *DepartureTracker) record(response *StopMonitoringResponse) {
	visits := response.Visits()
	if len(visits) == 0 {
		log.Println("Departure poll returned no stop visits")
		return
	}

	scheduleTypes := make(map[time.Time]ScheduleType)
	stored, skipped, failed := 0, 0, 0
	for _, visit := range visits {
		row, ok := buildDepartureRow(visit, "")
		if !ok {
			skipped++
			continue
		}
		row.ScheduleType = string(t.scheduleTypeFor(row.ServiceDate, scheduleTypes))
		if err := UpsertTrainDeparture(row); err != nil {
			log.Printf("failed to persist departure %s@%s: %v", row.TrainNumber, row.StopID, err)
			failed++
			continue
		}
		stored++
	}
	log.Printf("departures observed: %d stored, %d failed, %d skipped (incomplete)", stored, failed, skipped)
}

// scheduleTypeFor resolves the schedule type for a service date, memoising
// within a single poll. An empty result means the holiday calendar was
// unavailable; day_of_week is still stored, so only the holiday flag is lost.
func (t *DepartureTracker) scheduleTypeFor(date time.Time, memo map[time.Time]ScheduleType) ScheduleType {
	if cached, found := memo[date]; found {
		return cached
	}
	resolved := ScheduleType("")
	holidays, err := getHolidays(t.pool, departureOperatorID)
	if err != nil {
		log.Printf("Warning: schedule type unavailable for %s: %v", date.Format("2006-01-02"), err)
	} else {
		resolved = DetermineScheduleType(holidays, date)
	}
	memo[date] = resolved
	return resolved
}

// Finalize stamps rows whose stop visit has been absent long enough to conclude
// the train has departed.
//
// The sweep is skipped when the last successful poll is itself older than the
// grace window: during an outage every row looks stale, and finalizing them
// would freeze live trains at whatever prediction was last seen.
func (t *DepartureTracker) Finalize() {
	grace := t.finalizeInterval()

	t.mu.RLock()
	last := t.lastSuccessfulPoll
	t.mu.RUnlock()

	if time.Since(last) > grace {
		log.Println("Skipping departure finalization: no recent successful poll")
		return
	}

	finalized, err := FinalizeStaleDepartures(grace)
	if err != nil {
		log.Printf("Warning: failed to finalize departures: %v", err)
		return
	}
	if finalized > 0 {
		log.Printf("departures finalized: %d", finalized)
	}
}
